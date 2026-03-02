package controller

import (
	"fmt"
	"maps"

	klonev1alpha1 "github.com/klone/operator/api/v1alpha1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildIngress creates an Ingress resource based on the ingress type
func BuildIngress(cluster *klonev1alpha1.KloneCluster) *networkingv1.Ingress {
	if cluster.Spec.Ingress == nil {
		return nil
	}

	ingressType := cluster.Spec.Ingress.Type
	if ingressType == "" {
		ingressType = IngressTypeTailscale
	}

	switch ingressType {
	case IngressTypeTailscale:
		return BuildTailscaleIngress(cluster)
	case IngressTypeLoadBalancer:
		return BuildLoadBalancerIngress(cluster)
	case IngressTypeNone:
		return nil
	default:
		return nil
	}
}

// BuildTailscaleIngress creates a Tailscale Ingress
func BuildTailscaleIngress(cluster *klonev1alpha1.KloneCluster) *networkingv1.Ingress {
	namespaceName := GetNamespaceName(cluster.Name)

	// Get Tailscale configuration
	domain := ""
	tags := []string{"tag:k8s-operator", "tag:k8s"}
	annotations := make(map[string]string)

	if cluster.Spec.Ingress.Tailscale != nil {
		if cluster.Spec.Ingress.Tailscale.Domain != "" {
			domain = cluster.Spec.Ingress.Tailscale.Domain
		}
		if len(cluster.Spec.Ingress.Tailscale.Tags) > 0 {
			tags = cluster.Spec.Ingress.Tailscale.Tags
		}
		if cluster.Spec.Ingress.Tailscale.Annotations != nil {
			annotations = cluster.Spec.Ingress.Tailscale.Annotations
		}
	}

	// Build hostname
	hostname := cluster.Name
	if domain != "" {
		hostname = fmt.Sprintf("%s-terminal", cluster.Name)
	}

	// Set default annotations
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Add Tailscale-specific annotations
	tagsStr := ""
	for i, tag := range tags {
		if i > 0 {
			tagsStr += ","
		}
		tagsStr += tag
	}
	if tagsStr != "" {
		annotations["tailscale.com/tags"] = tagsStr
	}

	pathTypePrefix := networkingv1.PathTypePrefix

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        GetIngressName(),
			Namespace:   namespaceName,
			Annotations: annotations,
			Labels: map[string]string{
				"app":                "klone-terminal",
				"klone-cluster-name": cluster.Name,
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: stringPtr("tailscale"),
			TLS: []networkingv1.IngressTLS{
				{
					Hosts: []string{hostname},
				},
			},
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: GetTerminalServiceName(),
											Port: networkingv1.ServiceBackendPort{
												Name: "http",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// BuildLoadBalancerIngress creates an AWS ALB Ingress
func BuildLoadBalancerIngress(cluster *klonev1alpha1.KloneCluster) *networkingv1.Ingress {
	namespaceName := GetNamespaceName(cluster.Name)

	// Get LoadBalancer configuration
	annotations := map[string]string{
		"kubernetes.io/ingress.class":            "alb",
		"alb.ingress.kubernetes.io/scheme":       "internet-facing",
		"alb.ingress.kubernetes.io/target-type":  "ip",
		"alb.ingress.kubernetes.io/listen-ports": `[{"HTTPS":443}]`,
	}

	if cluster.Spec.Ingress.LoadBalancer != nil {
		if cluster.Spec.Ingress.LoadBalancer.Scheme != "" {
			annotations["alb.ingress.kubernetes.io/scheme"] = cluster.Spec.Ingress.LoadBalancer.Scheme
		}

		// Add certificate ARN if provided
		if cluster.Spec.Ingress.LoadBalancer.CertificateArn != "" {
			annotations["alb.ingress.kubernetes.io/certificate-arn"] = cluster.Spec.Ingress.LoadBalancer.CertificateArn
		}

		// Add external-dns configuration
		if cluster.Spec.Ingress.LoadBalancer.ExternalDNS != nil {
			if cluster.Spec.Ingress.LoadBalancer.ExternalDNS.Hostname != "" {
				annotations["external-dns.alpha.kubernetes.io/hostname"] = cluster.Spec.Ingress.LoadBalancer.ExternalDNS.Hostname
			}
			if cluster.Spec.Ingress.LoadBalancer.ExternalDNS.TTL > 0 {
				annotations["external-dns.alpha.kubernetes.io/ttl"] = fmt.Sprintf("%d", cluster.Spec.Ingress.LoadBalancer.ExternalDNS.TTL)
			}
		}

		// Merge user-provided annotations
		if cluster.Spec.Ingress.LoadBalancer.Annotations != nil {
			maps.Copy(annotations, cluster.Spec.Ingress.LoadBalancer.Annotations)
		}
	}

	pathTypePrefix := networkingv1.PathTypePrefix

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        GetIngressName(),
			Namespace:   namespaceName,
			Annotations: annotations,
			Labels: map[string]string{
				"app":                "klone-terminal",
				"klone-cluster-name": cluster.Name,
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: stringPtr("alb"),
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: GetTerminalServiceName(),
											Port: networkingv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Add hostname to rule if specified
	if cluster.Spec.Ingress.LoadBalancer != nil &&
		cluster.Spec.Ingress.LoadBalancer.ExternalDNS != nil &&
		cluster.Spec.Ingress.LoadBalancer.ExternalDNS.Hostname != "" {
		ingress.Spec.Rules[0].Host = cluster.Spec.Ingress.LoadBalancer.ExternalDNS.Hostname
	}

	return ingress
}

func stringPtr(s string) *string {
	return &s
}
