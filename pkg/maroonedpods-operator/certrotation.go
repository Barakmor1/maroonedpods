package maroonedpods_operator

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/openshift/library-go/pkg/crypto"
	"github.com/openshift/library-go/pkg/operator/certrotation"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/client-go/kubernetes"
	listerscorev1 "k8s.io/client-go/listers/core/v1"
	toolscache "k8s.io/client-go/tools/cache"
	mpcerts "maroonedpods.io/maroonedpods/pkg/maroonedpods-operator/resources/cert"

	"sigs.k8s.io/controller-runtime/pkg/manager"
	"time"
)

const (
	annCertConfig = "operator.maroonedpods.io/certConfig"
)

// CertManager is the client interface to the certificate manager/refresher
type CertManager interface {
	Sync(certs []mpcerts.CertificateDefinition) error
}

type certListers struct {
	secretLister    listerscorev1.SecretLister
	configMapLister listerscorev1.ConfigMapLister
}

type certManager struct {
	namespaces []string
	listerMap  map[string]*certListers

	k8sClient     kubernetes.Interface
	informers     v1helpers.KubeInformersForNamespaces
	eventRecorder events.Recorder
}

type serializedCertConfig struct {
	Lifetime string `json:"lifetime,omitempty"`
	Refresh  string `json:"refresh,omitempty"`
}

// NewCertManager creates a new certificate manager/refresher
func NewCertManager(mgr manager.Manager, installNamespace string, additionalNamespaces ...string) (CertManager, error) {
	k8sClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, err
	}

	cm := newCertManager(k8sClient, installNamespace, additionalNamespaces...)

	// so we can start caches
	if err = mgr.Add(cm); err != nil {
		return nil, err
	}

	return cm, nil
}

func newCertManager(client kubernetes.Interface, installNamespace string, additionalNamespaces ...string) *certManager {
	namespaces := append(additionalNamespaces, installNamespace)
	informers := v1helpers.NewKubeInformersForNamespaces(client, namespaces...)

	controllerRef, err := events.GetControllerReferenceForCurrentPod(context.TODO(), client, installNamespace, nil)
	if err != nil {
		log.Info("Unable to get controller reference, using namespace")
	}

	eventRecorder := events.NewRecorder(client.CoreV1().Events(installNamespace), installNamespace, controllerRef)

	return &certManager{
		namespaces:    namespaces,
		k8sClient:     client,
		informers:     informers,
		eventRecorder: eventRecorder,
	}
}

func (cm *certManager) Start(ctx context.Context) error {
	cm.informers.Start(ctx.Done())

	for _, ns := range cm.namespaces {
		secretInformer := cm.informers.InformersFor(ns).Core().V1().Secrets().Informer()
		go secretInformer.Run(ctx.Done())

		configMapInformer := cm.informers.InformersFor(ns).Core().V1().ConfigMaps().Informer()
		go configMapInformer.Run(ctx.Done())

		if !toolscache.WaitForCacheSync(ctx.Done(), secretInformer.HasSynced, configMapInformer.HasSynced) {
			return fmt.Errorf("could not sync informer cache")
		}

		if cm.listerMap == nil {
			cm.listerMap = make(map[string]*certListers)
		}

		cm.listerMap[ns] = &certListers{
			secretLister:    cm.informers.InformersFor(ns).Core().V1().Secrets().Lister(),
			configMapLister: cm.informers.InformersFor(ns).Core().V1().ConfigMaps().Lister(),
		}
	}

	return nil
}

func (cm *certManager) Sync(certs []mpcerts.CertificateDefinition) error {
	for _, cd := range certs {
		ca, err := cm.ensureSigner(cd)
		if err != nil {
			return err
		}

		if cd.CertBundleConfigmap == nil {
			continue
		}

		bundle, err := cm.ensureCertBundle(cd, ca)
		if err != nil {
			return err
		}

		if cd.TargetSecret == nil {
			continue
		}

		if err := cm.ensureTarget(cd, ca, bundle); err != nil {
			return err
		}
	}

	return nil
}

func (cm *certManager) ensureSigner(cd mpcerts.CertificateDefinition) (*crypto.CA, error) {
	listers, ok := cm.listerMap[cd.SignerSecret.Namespace]
	if !ok {
		return nil, fmt.Errorf("no lister for namespace %s", cd.SignerSecret.Namespace)
	}
	lister := listers.secretLister
	secret, err := lister.Secrets(cd.SignerSecret.Namespace).Get(cd.SignerSecret.Name)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}

		secret, err = cm.createSecret(cd.SignerSecret.Namespace, cd.SignerSecret.Name)
		if err != nil {
			return nil, err
		}
	}

	if secret, err = cm.ensureCertConfig(secret, cd.SignerConfig); err != nil {
		return nil, err
	}

	sr := certrotation.RotatedSigningCASecret{
		Name:          secret.Name,
		Namespace:     secret.Namespace,
		Validity:      cd.SignerConfig.Lifetime,
		Refresh:       cd.SignerConfig.Refresh,
		Lister:        lister,
		Client:        cm.k8sClient.CoreV1(),
		EventRecorder: cm.eventRecorder,
	}

	ca, err := sr.EnsureSigningCertKeyPair(context.TODO())
	if err != nil {
		return nil, err
	}

	return ca, nil
}

func (cm *certManager) createSecret(namespace, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	return cm.k8sClient.CoreV1().Secrets(namespace).Create(context.TODO(), secret, metav1.CreateOptions{})
}

func (cm *certManager) ensureCertConfig(secret *corev1.Secret, certConfig mpcerts.CertificateConfig) (*corev1.Secret, error) {
	scc := &serializedCertConfig{
		Lifetime: certConfig.Lifetime.String(),
		Refresh:  certConfig.Refresh.String(),
	}

	configBytes, err := json.Marshal(scc)
	if err != nil {
		return nil, err
	}

	configString := string(configBytes)
	currentConfig := secret.Annotations[annCertConfig]
	if currentConfig == configString {
		return secret, nil
	}

	secretCpy := secret.DeepCopy()

	if secretCpy.Annotations == nil {
		secretCpy.Annotations = make(map[string]string)
	}

	// force refresh
	if _, ok := secretCpy.Annotations[certrotation.CertificateNotAfterAnnotation]; ok {
		secretCpy.Annotations[certrotation.CertificateNotAfterAnnotation] = time.Now().Format(time.RFC3339)
	}
	secretCpy.Annotations[annCertConfig] = configString

	if secret, err = cm.k8sClient.CoreV1().Secrets(secretCpy.Namespace).Update(context.TODO(), secretCpy, metav1.UpdateOptions{}); err != nil {
		return nil, err
	}

	return secret, nil
}

func (cm *certManager) ensureCertBundle(cd mpcerts.CertificateDefinition, ca *crypto.CA) ([]*x509.Certificate, error) {
	configMap := cd.CertBundleConfigmap
	listers, ok := cm.listerMap[configMap.Namespace]
	if !ok {
		return nil, fmt.Errorf("no lister for namespace %s", configMap.Namespace)
	}
	lister := listers.configMapLister
	br := certrotation.CABundleConfigMap{
		Name:          configMap.Name,
		Namespace:     configMap.Namespace,
		Lister:        lister,
		Client:        cm.k8sClient.CoreV1(),
		EventRecorder: cm.eventRecorder,
	}

	certs, err := br.EnsureConfigMapCABundle(context.TODO(), ca)
	if err != nil {
		return nil, err
	}

	return certs, nil
}

func (cm *certManager) ensureTarget(cd mpcerts.CertificateDefinition, ca *crypto.CA, bundle []*x509.Certificate) error {
	listers, ok := cm.listerMap[cd.SignerSecret.Namespace]
	if !ok {
		return fmt.Errorf("no lister for namespace %s", cd.SignerSecret.Namespace)
	}
	lister := listers.secretLister
	secret, err := lister.Secrets(cd.TargetSecret.Namespace).Get(cd.TargetSecret.Name)
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		secret, err = cm.createSecret(cd.TargetSecret.Namespace, cd.TargetSecret.Name)
		if err != nil {
			return err
		}
	}

	if secret, err = cm.ensureCertConfig(secret, cd.TargetConfig); err != nil {
		return err
	}

	var targetCreator certrotation.TargetCertCreator
	if cd.TargetService != nil {
		targetCreator = &certrotation.ServingRotation{
			Hostnames: func() []string {
				return []string{
					*cd.TargetService,
					fmt.Sprintf("%s.%s", *cd.TargetService, secret.Namespace),
					fmt.Sprintf("%s.%s.svc", *cd.TargetService, secret.Namespace),
				}
			},
		}
	} else {
		targetCreator = &certrotation.ClientRotation{
			UserInfo: &user.DefaultInfo{Name: *cd.TargetUser},
		}
	}

	tr := certrotation.RotatedSelfSignedCertKeySecret{
		Name:          secret.Name,
		Namespace:     secret.Namespace,
		Validity:      cd.TargetConfig.Lifetime,
		Refresh:       cd.TargetConfig.Refresh,
		CertCreator:   targetCreator,
		Lister:        lister,
		Client:        cm.k8sClient.CoreV1(),
		EventRecorder: cm.eventRecorder,
	}

	if err := tr.EnsureTargetCertKeyPair(context.TODO(), ca, bundle); err != nil {
		return err
	}

	return nil
}
