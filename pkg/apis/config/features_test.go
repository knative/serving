/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	. "knative.dev/pkg/configmap/testing"
	_ "knative.dev/pkg/system/testing"
)

func TestFeaturesConfigurationFromFile(t *testing.T) {
	cm, example := ConfigMapsFromTestFile(t, FeaturesConfigName)

	if _, err := NewFeaturesConfigFromConfigMap(cm); err != nil {
		t.Error("NewFeaturesConfigFromConfigMap(actual) =", err)
	}

	got, err := NewFeaturesConfigFromConfigMap(example)
	if err != nil {
		t.Fatal("NewFeaturesConfigFromConfigMap(example) =", err)
	}

	want := defaultFeaturesConfig()
	if diff := cmp.Diff(want, got); diff != "" {
		t.Error("Example does not represent default config: diff(-want,+got)\n", diff)
	}
}

func TestFeaturesConfiguration(t *testing.T) {
	configTests := []struct {
		name         string
		wantErr      bool
		wantFeatures *Features
		data         map[string]string
	}{{
		name:         "default configuration",
		wantErr:      false,
		wantFeatures: defaultFeaturesConfig(),
		data:         map[string]string{},
	}, {
		name:    "features Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			MultiContainer:                   Enabled,
			MultiContainerProbing:            Enabled,
			PodSpecAffinity:                  Enabled,
			PodSpecTopologySpreadConstraints: Enabled,
			PodSpecDryRun:                    Enabled,
			PodSpecHostAliases:               Enabled,
			PodSpecNodeSelector:              Enabled,
			PodSpecRuntimeClassName:          Enabled,
			PodSpecSecurityContext:           Enabled,
			PodSpecShareProcessNamespace:     Enabled,
			PodSpecTolerations:               Enabled,
			PodSpecPriorityClassName:         Enabled,
			PodSpecSchedulerName:             Enabled,
			PodSpecDNSPolicy:                 Enabled,
			PodSpecDNSConfig:                 Enabled,
			SecurePodDefaults:                Enabled,
			QueueProxyResourceDefaults:       Enabled,
			TagHeaderBasedRouting:            Enabled,
		}),
		data: map[string]string{
			"multi-container":                              "Enabled",
			"multi-container-probing":                      "Enabled",
			"kubernetes.podspec-affinity":                  "Enabled",
			"kubernetes.podspec-topologyspreadconstraints": "Enabled",
			"kubernetes.podspec-dryrun":                    "Enabled",
			"kubernetes.podspec-hostaliases":               "Enabled",
			"kubernetes.podspec-nodeselector":              "Enabled",
			"kubernetes.podspec-runtimeclassname":          "Enabled",
			"kubernetes.podspec-securitycontext":           "Enabled",
			"kubernetes.podspec-shareprocessnamespace":     "Enabled",
			"kubernetes.podspec-tolerations":               "Enabled",
			"kubernetes.podspec-priorityclassname":         "Enabled",
			"kubernetes.podspec-schedulername":             "Enabled",
			"kubernetes.podspec-dnspolicy":                 "Enabled",
			"kubernetes.podspec-dnsconfig":                 "Enabled",
			"secure-pod-defaults":                          "Enabled",
			"queueproxy.resource-defaults":                 "Enabled",
			"tag-header-based-routing":                     "Enabled",
		},
	}, {
		name:    "multi-container Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			MultiContainer: Allowed,
		}),
		data: map[string]string{
			"multi-container": "Allowed",
		},
	}, {
		name:    "multi-container Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			MultiContainer: Disabled,
		}),
		data: map[string]string{
			"multi-container": "Disabled",
		},
	}, {
		name:    "multi-container-probing Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			MultiContainerProbing: Allowed,
		}),
		data: map[string]string{
			"multi-container-probing": "Allowed",
		},
	}, {
		name:    "multi-container-probing Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			MultiContainerProbing: Disabled,
		}),
		data: map[string]string{
			"multi-container-probing": "Disabled",
		},
	}, {
		name:    "kubernetes.podspec-affinity Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecAffinity: Allowed,
		}),
		data: map[string]string{
			"kubernetes.podspec-affinity": "Allowed",
		},
	}, {
		name:    "kubernetes.podspec-affinity Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecAffinity: Enabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-affinity": "Enabled",
		},
	}, {
		name:    "kubernetes.podspec-affinity Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecAffinity: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-affinity": "Disabled",
		},
	}, {
		name:    "kubernetes.podspec-topologyspreadconstraints Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecTopologySpreadConstraints: Allowed,
		}),
		data: map[string]string{
			"kubernetes.podspec-topologyspreadconstraints": "Allowed",
		},
	}, {
		name:    "kubernetes.podspec-topologyspreadconstraints Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecTopologySpreadConstraints: Enabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-topologyspreadconstraints": "Enabled",
		},
	}, {
		name:    "kubernetes.podspec-topologyspreadconstraints Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecTopologySpreadConstraints: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-topologyspreadconstraints": "Disabled",
		},
	}, {
		name:    "kubernetes.podspec-fieldref Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecFieldRef: Allowed,
		}),
		data: map[string]string{
			"kubernetes.podspec-fieldref": "Allowed",
		},
	}, {
		name:    "kubernetes.podspec-fieldref Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecFieldRef: Enabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-fieldref": "Enabled",
		},
	}, {
		name:    "kubernetes.podspec-fieldref Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecFieldRef: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-fieldref": "Disabled",
		},
	}, {
		name:    "kubernetes.podspec-dryrun Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecDryRun: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-dryrun": "Disabled",
		},
	}, {
		name:    "kubernetes.podspec-hostaliases Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecHostAliases: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-hostaliases": "Disabled",
		},
	}, {
		name:    "kubernetes.podspec-hostaliases Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecHostAliases: Allowed,
		}),
		data: map[string]string{
			"kubernetes.podspec-hostaliases": "Allowed",
		},
	}, {
		name:    "kubernetes.podspec-hostaliases Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecHostAliases: Enabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-hostaliases": "Enabled",
		},
	}, {
		name:    "kubernetes.podspec-nodeselector Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecNodeSelector: Allowed,
		}),
		data: map[string]string{
			"kubernetes.podspec-nodeselector": "Allowed",
		},
	}, {
		name:    "kubernetes.podspec-nodeselector Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecNodeSelector: Enabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-nodeselector": "Enabled",
		},
	}, {
		name:    "kubernetes.podspec-nodeselector Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecNodeSelector: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-nodeselector": "Disabled",
		},
	}, {
		name:    "kubernetes.podspec-runtimeclassname Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecRuntimeClassName: Allowed,
		}),
		data: map[string]string{
			"kubernetes.podspec-runtimeclassname": "Allowed",
		},
	}, {
		name:    "kubernetes.podspec-runtimeclassname Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecRuntimeClassName: Enabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-runtimeclassname": "Enabled",
		},
	}, {
		name:    "kubernetes.podspec-runtimeclassname Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecRuntimeClassName: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-runtimeclassname": "Disabled",
		},
	}, {
		name:    "kubernetes.podspec-tolerations Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecTolerations: Allowed,
		}),
		data: map[string]string{
			"kubernetes.podspec-tolerations": "Allowed",
		},
	}, {
		name:    "kubernetes.podspec-tolerations Enabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecTolerations: Enabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-tolerations": "Enabled",
		},
	}, {
		name:    "kubernetes.podspec-tolerations Disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecTolerations: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-tolerations": "Disabled",
		},
	}, {
		name:    "security context Allowed",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecSecurityContext: Allowed,
		}),
		data: map[string]string{
			"kubernetes.podspec-securitycontext": "Allowed",
		},
	}, {
		name:    "security context disabled",
		wantErr: false,
		wantFeatures: defaultWith(&Features{
			PodSpecSecurityContext: Disabled,
		}),
		data: map[string]string{
			"kubernetes.podspec-securitycontext": "Disabled",
		},
	},
		{
			name:    "shared process namespace Allowed",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecShareProcessNamespace: Allowed,
			}),
			data: map[string]string{
				"kubernetes.podspec-shareprocessnamespace": "Allowed",
			},
		}, {
			name:    "shared process namespace Disabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecShareProcessNamespace: Disabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-shareprocessnamespace": "Disabled",
			},
		}, {
			name:    "kubernetes.containerspec-addcapabilities Disabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				ContainerSpecAddCapabilities: Disabled,
			}),
			data: map[string]string{
				"kubernetes.containerspec-addcapabilities": "Disabled",
			},
		}, {
			name:    "kubernetes.containerspec-addcapabilities Enabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				ContainerSpecAddCapabilities: Enabled,
			}),
			data: map[string]string{
				"kubernetes.containerspec-addcapabilities": "Enabled",
			},
		}, {
			name:    "kubernetes.containerspec-addcapabilities Allowed",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				ContainerSpecAddCapabilities: Allowed,
			}),
			data: map[string]string{
				"kubernetes.containerspec-addcapabilities": "Allowed",
			},
		}, {
			name:    "tag-header-based-routing Allowed",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				TagHeaderBasedRouting: Allowed,
			}),
			data: map[string]string{
				"tag-header-based-routing": "Allowed",
			},
		}, {
			name:    "tag-header-based-routing Enabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				TagHeaderBasedRouting: Enabled,
			}),
			data: map[string]string{
				"tag-header-based-routing": "Enabled",
			},
		}, {
			name:    "kubernetes.podspec-volumes-emptyDir Disabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecVolumesEmptyDir: Disabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-volumes-emptydir": "Disabled",
			},
		}, {
			name:    "kubernetes.podspec-volumes-emptyDir Enabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecVolumesEmptyDir: Enabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-volumes-emptydir": "Enabled",
			},
		}, {
			name:    "kubernetes.podspec-persistent-volume-claim Disabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecPersistentVolumeClaim: Disabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-persistent-volume-claim": "Disabled",
			},
		}, {
			name:    "kubernetes.podspec-persistent-volume-claim Enabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecPersistentVolumeClaim: Enabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-persistent-volume-claim": "Enabled",
			},
		}, {
			name:    "kubernetes.podspec-persistent-volume-write Disabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecPersistentVolumeWrite: Disabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-persistent-volume-write": "Disabled",
			},
		}, {
			name:    "kubernetes.podspec-persistent-volume-claim Enabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecPersistentVolumeWrite: Enabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-persistent-volume-write": "Enabled",
			},
		}, {
			name:    "kubernetes.podspec-init-containers Disabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecInitContainers: Disabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-init-containers": "Disabled",
			},
		}, {
			name:    "kubernetes.podspec-init-container Enabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecInitContainers: Enabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-init-containers": "Enabled",
			},
		}, {
			name:    "kubernetes.podspec-priorityclassname Allowed",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecPriorityClassName: Allowed,
			}),
			data: map[string]string{
				"kubernetes.podspec-priorityclassname": "Allowed",
			},
		}, {
			name:    "kubernetes.podspec-priorityclassname Enabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecPriorityClassName: Enabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-priorityclassname": "Enabled",
			},
		}, {
			name:    "kubernetes.podspec-priorityclassname Disabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecPriorityClassName: Disabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-priorityclassname": "Disabled",
			},
		}, {
			name:    "kubernetes.podspec-schedulername Allowed",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecSchedulerName: Allowed,
			}),
			data: map[string]string{
				"kubernetes.podspec-schedulername": "Allowed",
			},
		}, {
			name:    "kubernetes.podspec-schedulername Enabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecSchedulerName: Enabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-schedulername": "Enabled",
			},
		}, {
			name:    "kubernetes.podspec-schedulername Disabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecSchedulerName: Disabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-schedulername": "Disabled",
			},
		}, {
			name:    "kubernetes.podspec-dnspolicy Allowed",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecDNSPolicy: Allowed,
			}),
			data: map[string]string{
				"kubernetes.podspec-dnspolicy": "Allowed",
			},
		}, {
			name:    "kubernetes.podspec-dnspolicy Enabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecDNSPolicy: Enabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-dnspolicy": "Enabled",
			},
		}, {
			name:    "kubernetes.podspec-dnspolicy Disabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecDNSPolicy: Disabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-dnspolicy": "Disabled",
			},
		}, {
			name:    "kubernetes.podspec-dnsconfig Allowed",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecDNSConfig: Allowed,
			}),
			data: map[string]string{
				"kubernetes.podspec-dnsconfig": "Allowed",
			},
		}, {
			name:    "kubernetes.podspec-dnsconfig Enabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecDNSConfig: Enabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-dnsconfig": "Enabled",
			},
		}, {
			name:    "kubernetes.podspec-dnsconfig Disabled",
			wantErr: false,
			wantFeatures: defaultWith(&Features{
				PodSpecDNSConfig: Disabled,
			}),
			data: map[string]string{
				"kubernetes.podspec-dnsconfig": "Disabled",
			},
		}}

	for _, tt := range configTests {
		t.Run(tt.name, func(t *testing.T) {
			actualFeatures, err := NewFeaturesConfigFromConfigMap(&corev1.ConfigMap{
				Data: tt.data,
			})

			if (err != nil) != tt.wantErr {
				t.Fatalf("NewFeaturesConfigFromConfigMap() error = %v, WantErr %v", err, tt.wantErr)
			}

			got, want := actualFeatures, tt.wantFeatures
			if diff := cmp.Diff(want, got); diff != "" {
				t.Error("Config mismatch: diff(-want,+got):\n", diff)
			}
		})
	}
}

// defaultWith returns the default *Feature patched with the provided *Features.
func defaultWith(p *Features) *Features {
	f := defaultFeaturesConfig()
	pType := reflect.ValueOf(p).Elem()
	fType := reflect.ValueOf(f).Elem()
	for i := 0; i < pType.NumField(); i++ {
		if pType.Field(i).Interface().(Flag) != "" {
			fType.Field(i).Set(pType.Field(i))
		}
	}
	return f
}
