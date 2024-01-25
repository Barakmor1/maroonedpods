package cluster

import (
	rbacv1 "k8s.io/api/rbac/v1"
	utils2 "maroonedpods.io/maroonedpods/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func createStaticControllerResources(args *FactoryArgs) []client.Object {
	return []client.Object{
		createControllerClusterRole(),
		createControllerClusterRoleBinding(args.Namespace),
	}
}

func createControllerClusterRoleBinding(namespace string) *rbacv1.ClusterRoleBinding {
	return utils2.ResourceBuilder.CreateClusterRoleBinding(utils2.ControllerServiceAccountName, utils2.ControllerClusterRoleName, utils2.ControllerServiceAccountName, namespace)
}

func getControllerClusterPolicyRules() []rbacv1.PolicyRule {
	return []rbacv1.PolicyRule{
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"events",
			},
			Verbs: []string{
				"create",
				"patch",
			},
		},
        {
            APIGroups: []string{
                "",
            },
            Resources: []string{
                "nodes",
            },
            Verbs: []string{
                "get", "list", "watch", "update", "patch",
            },
        },
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"pods",
			},
			Verbs: []string{
				"update",
				"list",
				"watch",
				"get",
                "patch",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"persistentvolumeclaims",
			},
			Verbs: []string{
				"list",
				"watch",
				"get",
			},
		},
		{
			APIGroups: []string{
				"apiextensions.k8s.io",
			},
			Resources: []string{
				"customresourcedefinitions",
			},
			Verbs: []string{
				"list",
				"watch",
				"get",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"resourcequotas",
			},
			Verbs: []string{
				"list",
				"watch",
				"update",
				"create",
				"delete",
			},
		},
		{
			APIGroups: []string{
				"maroonedpods.io",
			},
			Resources: []string{
				"maroonedpods",
			},
			Verbs: []string{
				"get",
				"update",
				"watch",
				"list",
                "delete",
                "patch",
			},
		},
		{
			APIGroups: []string{
				"maroonedpods.io",
			},
			Resources: []string{
				"maroonedpods/status",
			},
			Verbs: []string{
				"update",
                "patch", 
			},
		},
		{
			APIGroups: []string{
				"kubevirt.io",
			},
			Resources: []string{
				"kubevirts",
			},
			Verbs: []string{
				"list",
				"watch",
			},
		},
		{
			APIGroups: []string{
				"kubevirt.io",
			},
			Resources: []string{
				"virtualmachineinstances",
			},
			Verbs: []string{
				"watch",
				"list",
				"get",
                "create",
                "update",
                "delete",
                "patch",
			},
		},
		{
			APIGroups: []string{
				"kubevirt.io",
			},
			Resources: []string{
				"virtualmachineinstances/status",
			},
			Verbs: []string{
                "patch",
			},
		},
		{
			APIGroups: []string{
				"admissionregistration.k8s.io",
			},
			Resources: []string{
				"validatingwebhookconfigurations",
			},
			Verbs: []string{
				"create",
				"get",
				"delete",
			},
		},
		{
			APIGroups: []string{
				"maroonedpods.io",
			},
			Resources: []string{
				"mps",
			},
			Verbs: []string{
				"list",
				"watch",
			},
		},
	}
}

func createControllerClusterRole() *rbacv1.ClusterRole {
	return utils2.ResourceBuilder.CreateClusterRole(utils2.ControllerClusterRoleName, getControllerClusterPolicyRules())
}
