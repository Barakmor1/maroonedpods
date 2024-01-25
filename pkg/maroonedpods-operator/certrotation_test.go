package maroonedpods_operator

import (
	"context"
	"encoding/json"
	"maroonedpods.io/pkg/util"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/operator/certrotation"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"maroonedpods.io/pkg/maroonedpods-operator/resources/cert"
)

func newCertManagerForTest(client kubernetes.Interface, namespace string) CertManager {
	return newCertManager(client, namespace)
}

func toSerializedCertConfig(l, r time.Duration) string {
	scc := &serializedCertConfig{
		Lifetime: l.String(),
		Refresh:  r.String(),
	}

	bs, err := json.Marshal(scc)
	Expect(err).ToNot(HaveOccurred())
	return string(bs)
}

func getCertNotBefore(client kubernetes.Interface, namespace, name string) time.Time {
	s, err := client.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	val, ok := s.Annotations[certrotation.CertificateNotBeforeAnnotation]
	Expect(ok).To(BeTrue())
	t, err := time.Parse(time.RFC3339, val)
	Expect(err).ToNot(HaveOccurred())
	return t
}

func getCertConfigAnno(client kubernetes.Interface, namespace, name string) string {
	s, err := client.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	val, ok := s.Annotations[annCertConfig]
	Expect(ok).To(BeTrue())
	return val
}

func checkSecret(client kubernetes.Interface, namespace, name string, exists bool) {
	s, err := client.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if !exists {
		Expect(errors.IsNotFound(err)).To(BeTrue())
		return
	}
	Expect(err).ToNot(HaveOccurred())
	Expect(s.Data["tls.crt"]).ShouldNot(BeEmpty())
	Expect(s.Data["tls.crt"]).ShouldNot(BeEmpty())
}

func checkConfigMap(client kubernetes.Interface, namespace, name string, exists bool) {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if !exists {
		Expect(errors.IsNotFound(err)).To(BeTrue())
		return
	}
	Expect(cm.Data["ca-bundle.crt"]).ShouldNot(BeEmpty())
}

func checkCerts(client kubernetes.Interface, namespace string, exists bool) {
	checkSecret(client, namespace, "maroonedpods-server", exists)
	checkConfigMap(client, namespace, "maroonedpods-server-signer-bundle", exists)
	checkSecret(client, namespace, util.SecretResourceName, exists)
}

var _ = Describe("Cert rotation tests", func() {
	const namespace = "maroonedpods"

	pt := func(d time.Duration) *time.Duration {
		return &d
	}

	Context("with clean slate", func() {
		It("should create everything", func() {
			client := fake.NewSimpleClientset()
			cm := newCertManagerForTest(client, namespace)

			ctx, cancel := context.WithCancel(context.Background())
			Expect(cm.(*certManager).Start(ctx)).To(Succeed())

			checkCerts(client, namespace, false)

			certs := cert.CreateCertificateDefinitions(&cert.FactoryArgs{Namespace: namespace})
			err := cm.Sync(certs)
			Expect(err).ToNot(HaveOccurred())

			checkCerts(client, namespace, true)

			certs = cert.CreateCertificateDefinitions(&cert.FactoryArgs{Namespace: namespace})
			err = cm.Sync(certs)
			Expect(err).ToNot(HaveOccurred())

			checkCerts(client, namespace, true)

			cancel()
		})

		It("should update certs", func() {
			client := fake.NewSimpleClientset()
			cm := newCertManagerForTest(client, namespace)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			Expect(cm.(*certManager).Start(ctx)).To(Succeed())

			certs := cert.CreateCertificateDefinitions(&cert.FactoryArgs{Namespace: namespace})
			err := cm.Sync(certs)
			Expect(err).ToNot(HaveOccurred())

			apiCA := getCertNotBefore(client, namespace, "maroonedpods-server")
			apiServer := getCertNotBefore(client, namespace, util.SecretResourceName)

			n := time.Now()

			args := &cert.FactoryArgs{
				Namespace:         namespace,
				SignerDuration:    pt(50 * time.Hour),
				SignerRenewBefore: pt(25 * time.Hour),
				TargetDuration:    pt(26 * time.Hour),
				TargetRenewBefore: pt(13 * time.Hour),
			}

			certs = cert.CreateCertificateDefinitions(args)
			err = cm.Sync(certs)
			Expect(err).ToNot(HaveOccurred())

			apiCA2 := getCertNotBefore(client, namespace, "maroonedpods-server")
			apiServer2 := getCertNotBefore(client, namespace, util.SecretResourceName)

			Expect(apiCA2.After(n))
			Expect(apiServer2.After(n))

			Expect(apiCA2.After(apiCA))
			Expect(apiServer2.After(apiServer))

			apiCAConfig2 := getCertConfigAnno(client, namespace, "maroonedpods-server")
			apiServerConfig2 := getCertConfigAnno(client, namespace, util.SecretResourceName)

			scc := toSerializedCertConfig(50*time.Hour, 25*time.Hour)
			scc2 := toSerializedCertConfig(26*time.Hour, 13*time.Hour)

			Expect(apiCAConfig2).To(Equal(scc))
			Expect(apiServerConfig2).To(Equal(scc2))

		})
	})
})
