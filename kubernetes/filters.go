package kubernetes

import (
	"fmt"
	"strings"

	core_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/kiali/kiali/config"
)

// FilterPodsForService returns a subpart of pod list filtered according service selector
func FilterPodsForService(s *core_v1.Service, allPods []core_v1.Pod) []core_v1.Pod {
	if s == nil || allPods == nil {
		return nil
	}
	serviceSelector := labels.Set(s.Spec.Selector).AsSelector()
	pods := FilterPodsForSelector(serviceSelector, allPods)

	return pods
}

func FilterPodsForSelector(selector labels.Selector, allPods []core_v1.Pod) []core_v1.Pod {
	var pods []core_v1.Pod
	for _, pod := range allPods {
		if selector.Matches(labels.Set(pod.ObjectMeta.Labels)) {
			pods = append(pods, pod)
		}
	}
	return pods
}

// FilterPodsForEndpoints performs a second pass was selector may return too many data
// This case happens when a "nil" selector (such as one of default/kubernetes service) is used
func FilterPodsForEndpoints(endpoints *core_v1.Endpoints, unfiltered []core_v1.Pod) []core_v1.Pod {
	endpointPods := make(map[string]bool)
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
				endpointPods[address.TargetRef.Name] = true
			}
		}
	}
	var pods []core_v1.Pod
	for _, pod := range unfiltered {
		if _, ok := endpointPods[pod.Name]; ok {
			pods = append(pods, pod)
		}
	}
	return pods
}

func FilterPodsForController(controllerName string, controllerType string, allPods []core_v1.Pod) []core_v1.Pod {
	var pods []core_v1.Pod
	for _, pod := range allPods {
		for _, ref := range pod.OwnerReferences {
			if ref.Controller != nil && *ref.Controller && ref.Name == controllerName && ref.Kind == controllerType {
				pods = append(pods, pod)
				break
			}
		}
	}
	return pods
}

func FilterServicesForSelector(selector labels.Selector, allServices []core_v1.Service) []core_v1.Service {
	var services []core_v1.Service
	for _, svc := range allServices {
		if selector.Matches(labels.Set(svc.Spec.Selector)) {
			services = append(services, svc)
		}
	}
	return services
}

func FilterVirtualServices(allVs []IstioObject, namespace string, serviceName string) []IstioObject {
	typeMeta := meta_v1.TypeMeta{
		Kind:       PluralType[VirtualServices],
		APIVersion: ApiNetworkingVersion,
	}
	virtualServices := make([]IstioObject, 0)
	for _, virtualService := range allVs {
		appendVirtualService := serviceName == ""
		routeProtocols := []string{"http", "tcp"}
		if !appendVirtualService && FilterByRoute(virtualService.GetSpec(), routeProtocols, serviceName, namespace, nil) {
			appendVirtualService = true
		}
		if appendVirtualService {
			vs := virtualService.DeepCopyIstioObject()
			vs.SetTypeMeta(typeMeta)
			virtualServices = append(virtualServices, vs)
		}
	}
	return virtualServices
}

func FilterDestinationRules(allDr []IstioObject, namespace string, serviceName string) []IstioObject {
	typeMeta := meta_v1.TypeMeta{
		Kind:       PluralType[DestinationRules],
		APIVersion: ApiNetworkingVersion,
	}
	destinationRules := make([]IstioObject, 0)
	for _, destinationRule := range allDr {
		appendDestinationRule := serviceName == ""
		if host, ok := destinationRule.GetSpec()["host"]; ok {
			if dHost, ok := host.(string); ok && FilterByHost(dHost, serviceName, namespace) {
				appendDestinationRule = true
			}
		}
		if appendDestinationRule {
			dr := destinationRule.DeepCopyIstioObject()
			dr.SetTypeMeta(typeMeta)
			destinationRules = append(destinationRules, dr)
		}
	}
	return destinationRules
}

func FilterByHost(host, serviceName, namespace string) bool {
	// Check single name
	if host == serviceName {
		return true
	}
	// Check service.namespace
	if host == fmt.Sprintf("%s.%s", serviceName, namespace) {
		return true
	}
	// Check the FQDN. <service>.<namespace>.svc
	if host == fmt.Sprintf("%s.%s.%s", serviceName, namespace, "svc") {
		return true
	}

	// Check the FQDN. <service>.<namespace>.svc.<zone>
	if host == fmt.Sprintf("%s.%s.%s", serviceName, namespace, config.Get().ExternalServices.Istio.IstioIdentityDomain) {
		return true
	}

	// Note, FQDN names are defined from Kubernetes registry specification [1]
	// [1] https://github.com/kubernetes/dns/blob/master/docs/specification.md

	return false
}

func FilterByRoute(spec map[string]interface{}, protocols []string, service string, namespace string, serviceEntries map[string]struct{}) bool {
	if len(protocols) == 0 {
		return false
	}
	for _, protocol := range protocols {
		if prot, ok := spec[protocol]; ok {
			if aHttp, ok := prot.([]interface{}); ok {
				for _, httpRoute := range aHttp {
					if mHttpRoute, ok := httpRoute.(map[string]interface{}); ok {
						if route, ok := mHttpRoute["route"]; ok {
							if aRouteDestination, ok := route.([]interface{}); ok {
								for _, destination := range aRouteDestination {
									if mDestination, ok := destination.(map[string]interface{}); ok {
										if destinationW, ok := mDestination["destination"]; ok {
											if mDestinationW, ok := destinationW.(map[string]interface{}); ok {
												if host, ok := mDestinationW["host"]; ok {
													if sHost, ok := host.(string); ok {
														if FilterByHost(sHost, service, namespace) {
															return true
														}
														if serviceEntries != nil {
															// We have ServiceEntry to check
															if _, found := serviceEntries[strings.ToLower(protocol)+sHost]; found {
																return true
															}
														}
													}
												}
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	return false
}
