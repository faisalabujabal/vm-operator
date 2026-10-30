// © Broadcom. All Rights Reserved.
// The term "Broadcom" refers to Broadcom Inc. and/or its subsidiaries.
// SPDX-License-Identifier: Apache-2.0

package v1alpha5_test

import (
	"encoding/json"
	"fmt"

	"github.com/vmware-tanzu/vm-operator/api/utilconversion"
	vmopv1a1 "github.com/vmware-tanzu/vm-operator/api/v1alpha1"
	vmopv1a2 "github.com/vmware-tanzu/vm-operator/api/v1alpha2"
	vmopv1a3 "github.com/vmware-tanzu/vm-operator/api/v1alpha3"
	vmopv1a4 "github.com/vmware-tanzu/vm-operator/api/v1alpha4"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlconversion "sigs.k8s.io/controller-runtime/pkg/conversion"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	"k8s.io/apimachinery/pkg/runtime"
	runtimeserializer "k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/randfill"

	"github.com/vmware-tanzu/vm-operator/api/test/utilconversion/fuzztests"
	vmopv1a5 "github.com/vmware-tanzu/vm-operator/api/v1alpha5"
	vmopv1 "github.com/vmware-tanzu/vm-operator/api/v1alpha6"
	"github.com/vmware-tanzu/vm-operator/api/v1alpha6/common"
)

var _ = Describe("FuzzyConversion", Label("api", "fuzz"), func() {

	var (
		scheme *runtime.Scheme
		input  fuzztests.FuzzTestFuncInput
	)

	BeforeEach(func() {
		scheme = runtime.NewScheme()
		Expect(vmopv1a5.AddToScheme(scheme)).To(Succeed())
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
	})

	AfterEach(func() {
		input = fuzztests.FuzzTestFuncInput{}
	})

	Context("VirtualMachine", func() {
		BeforeEach(func() {
			input = fuzztests.FuzzTestFuncInput{
				Scheme: scheme,
				Hub:    &vmopv1.VirtualMachine{},
				Spoke:  &vmopv1a5.VirtualMachine{},
				FuzzerFuncs: []fuzzer.FuzzerFuncs{
					overrideVirtualMachineFieldsFuncs,
				},
			}
		})
		Context("Spoke-Hub-Spoke", func() {
			It("should get fuzzy with it", func() {
				fuzztests.SpokeHubSpoke(input)
			})
		})
		Context("Hub-Spoke-Hub", func() {
			It("should get fuzzy with it", func() {
				fuzztests.HubSpokeHub(input)
			})
		})
	})

	Context("VirtualMachineClass", func() {
		BeforeEach(func() {
			input = fuzztests.FuzzTestFuncInput{
				Scheme: scheme,
				HubAfterMutation: func(h ctrlconversion.Hub) {
					if m := h.(metav1.Object); len(m.GetAnnotations()) == 0 {
						m.SetAnnotations(nil)
					}
				},
				SpokeAfterMutation: func(h ctrlconversion.Convertible) {
					if m := h.(metav1.Object); len(m.GetAnnotations()) == 0 {
						m.SetAnnotations(nil)
					}
				},
				Hub:         &vmopv1.VirtualMachineClass{},
				Spoke:       &vmopv1a5.VirtualMachineClass{},
				FuzzerFuncs: []fuzzer.FuzzerFuncs{fuzztests.ClassPolicyFuzzerFuncs, overrideVirtualMachineFieldsFuncs},
			}
		})
		Context("Spoke-Hub-Spoke", func() {
			It("should get fuzzy with it", func() {
				fuzztests.SpokeHubSpoke(input)
			})
		})
		Context("Hub-Spoke-Hub", func() {
			It("should get fuzzy with it", func() {
				fuzztests.HubSpokeHub(input)
			})
		})
	})

	Context("VirtualMachineGroup", func() {
		BeforeEach(func() {
			input = fuzztests.FuzzTestFuncInput{
				Scheme: scheme,
				Hub:    &vmopv1.VirtualMachineGroup{},
				Spoke:  &vmopv1a5.VirtualMachineGroup{},
			}
		})
		Context("Spoke-Hub-Spoke", func() {
			It("should get fuzzy with it", func() {
				fuzztests.SpokeHubSpoke(input)
			})
		})
		Context("Hub-Spoke-Hub", func() {
			It("should get fuzzy with it", func() {
				fuzztests.HubSpokeHub(input)
			})
		})
	})

	Context("VirtualMachineImage", func() {
		BeforeEach(func() {
			input = fuzztests.FuzzTestFuncInput{
				Scheme: scheme,
				Hub:    &vmopv1.VirtualMachineImage{},
				Spoke:  &vmopv1a5.VirtualMachineImage{},
			}
		})
		Context("Spoke-Hub-Spoke", func() {
			It("should get fuzzy with it", func() {
				fuzztests.SpokeHubSpoke(input)
			})
		})
		Context("Hub-Spoke-Hub", func() {
			It("should get fuzzy with it", func() {
				fuzztests.HubSpokeHub(input)
			})
		})
	})

	Context("ClusterVirtualMachineImage", func() {
		BeforeEach(func() {
			input = fuzztests.FuzzTestFuncInput{
				Scheme: scheme,
				Hub:    &vmopv1.ClusterVirtualMachineImage{},
				Spoke:  &vmopv1a5.ClusterVirtualMachineImage{},
			}
		})
		Context("Spoke-Hub-Spoke", func() {
			It("should get fuzzy with it", func() {
				fuzztests.SpokeHubSpoke(input)
			})
		})
		Context("Hub-Spoke-Hub", func() {
			It("should get fuzzy with it", func() {
				fuzztests.HubSpokeHub(input)
			})
		})
	})

	Context("VirtualMachineImageCache", func() {
		BeforeEach(func() {
			input = fuzztests.FuzzTestFuncInput{
				Scheme: scheme,
				Hub:    &vmopv1.VirtualMachineImageCache{},
				Spoke:  &vmopv1a5.VirtualMachineImageCache{},
			}
		})
		Context("Spoke-Hub-Spoke", func() {
			It("should get fuzzy with it", func() {
				fuzztests.SpokeHubSpoke(input)
			})
		})
		Context("Hub-Spoke-Hub", func() {
			It("should get fuzzy with it", func() {
				fuzztests.HubSpokeHub(input)
			})
		})
	})

	Context("VirtualMachinePublishRequest", func() {
		BeforeEach(func() {
			input = fuzztests.FuzzTestFuncInput{
				Scheme: scheme,
				Hub:    &vmopv1.VirtualMachinePublishRequest{},
				Spoke:  &vmopv1a5.VirtualMachinePublishRequest{},
			}
		})
		Context("Spoke-Hub-Spoke", func() {
			It("should get fuzzy with it", func() {
				fuzztests.SpokeHubSpoke(input)
			})
		})
		Context("Hub-Spoke-Hub", func() {
			It("should get fuzzy with it", func() {
				fuzztests.HubSpokeHub(input)
			})
		})
	})

	Context("VirtualMachineService", func() {
		BeforeEach(func() {
			input = fuzztests.FuzzTestFuncInput{
				Scheme: scheme,
				Hub:    &vmopv1.VirtualMachineService{},
				Spoke:  &vmopv1a5.VirtualMachineService{},
			}
		})
		Context("Spoke-Hub-Spoke", func() {
			It("should get fuzzy with it", func() {
				fuzztests.SpokeHubSpoke(input)
			})
		})
		Context("Hub-Spoke-Hub", func() {
			It("should get fuzzy with it", func() {
				fuzztests.HubSpokeHub(input)
			})
		})
	})

	Context("VirtualMachineSetResourcePolicy", func() {
		BeforeEach(func() {
			input = fuzztests.FuzzTestFuncInput{
				Scheme: scheme,
				Hub:    &vmopv1.VirtualMachineSetResourcePolicy{},
				Spoke:  &vmopv1a5.VirtualMachineSetResourcePolicy{},
			}
		})
		Context("Spoke-Hub-Spoke", func() {
			It("should get fuzzy with it", func() {
				fuzztests.SpokeHubSpoke(input)
			})
		})
		Context("Hub-Spoke-Hub", func() {
			It("should get fuzzy with it", func() {
				fuzztests.HubSpokeHub(input)
			})
		})
	})

	Context("VirtualMachineWebConsoleRequest", func() {
		BeforeEach(func() {
			input = fuzztests.FuzzTestFuncInput{
				Scheme: scheme,
				Hub:    &vmopv1.VirtualMachineWebConsoleRequest{},
				Spoke:  &vmopv1a5.VirtualMachineWebConsoleRequest{},
			}
		})
		Context("Spoke-Hub-Spoke", func() {
			It("should get fuzzy with it", func() {
				fuzztests.SpokeHubSpoke(input)
			})
		})
		Context("Hub-Spoke-Hub", func() {
			It("should get fuzzy with it", func() {
				fuzztests.HubSpokeHub(input)
			})
		})
	})
})

var _ = Describe("Client-side conversion", func() {
	It("should convert VirtualMachine from current API version to latest API version", func() {
		scheme := runtime.NewScheme()
		Expect(vmopv1a5.AddToScheme(scheme)).To(Succeed())
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
		vm1 := &vmopv1a5.VirtualMachine{}
		vm2 := &vmopv1.VirtualMachine{}
		Expect(scheme.Convert(vm1, vm2, nil)).To(Succeed())
	})
})

func overrideVirtualMachineFieldsFuncs(codecs runtimeserializer.CodecFactory) []interface{} {
	return []interface{}{
		func(msg *json.RawMessage, c randfill.Continue) {
			*msg = []byte(`{"foo":"bar"}`)
		},
	}
}

var _ = Describe("VMClass schema migration", Label("api"), func() {
	for _, version := range []struct {
		name     string
		newSpoke func() ctrlconversion.Convertible
	}{
		{"v1alpha1", func() ctrlconversion.Convertible { return &vmopv1a1.VirtualMachineClass{} }},
		{"v1alpha2", func() ctrlconversion.Convertible { return &vmopv1a2.VirtualMachineClass{} }},
		{"v1alpha3", func() ctrlconversion.Convertible { return &vmopv1a3.VirtualMachineClass{} }},
		{"v1alpha4", func() ctrlconversion.Convertible { return &vmopv1a4.VirtualMachineClass{} }},
		{"v1alpha5", func() ctrlconversion.Convertible { return &vmopv1a5.VirtualMachineClass{} }},
	} {
		Context(version.name, func() {
			It("retains typed policies alongside unprojectable raw ExtraConfig", func() {
				for _, raw := range []string{`{"extraConfig":{"legacy":true}}`, `{"extraConfig":"legacy"}`} {
					hub := classWithPolicies()
					hub.Spec.ConfigSpec = json.RawMessage(raw)
					hub.Spec.Advanced = &vmopv1.VirtualMachineClassAdvancedSpec{PreferHTEnabled: vmopv1.FixedClassBool(false)}
					spoke := version.newSpoke()
					Expect(spoke.ConvertFrom(hub)).To(Succeed())
					encoded, err := json.Marshal(spoke)
					Expect(err).NotTo(HaveOccurred())
					var older struct {
						Spec struct{ ConfigSpec json.RawMessage }
					}
					Expect(json.Unmarshal(encoded, &older)).To(Succeed())
					Expect(older.Spec.ConfigSpec).To(MatchJSON(raw))
					var restored vmopv1.VirtualMachineClass
					Expect(spoke.ConvertTo(&restored)).To(Succeed())
					Expect(restored.Spec.ConfigSpec).To(MatchJSON(raw))
					Expect(restored.Spec.Advanced).To(Equal(hub.Spec.Advanced))
					Expect(restored.Spec.Resources).To(Equal(hub.Spec.Resources))
				}
			})
			It("preserves unrepresentable advanced raw leaves across old API reads", func() {
				for _, raw := range []string{
					`{"_typeName":"VirtualMachineConfigSpec","changeTrackingEnabled":"legacy"}`,
					`{"_typeName":"VirtualMachineConfigSpec","extraConfig":{"legacy":true}}`,
					`{"_typeName":"VirtualMachineConfigSpec","extraConfig":"legacy"}`,
					`{"_typeName":"VirtualMachineConfigSpec","extraConfig":["legacy",7,null,{"key":7,"value":"bad-key"},{"key":null,"value":"null-key"},{"key":"opaque","value":{"future":true}}]}`,
				} {
					spoke := version.newSpoke()
					Expect(json.Unmarshal([]byte(`{"spec":{"configSpec":`+raw+`}}`), spoke)).To(Succeed())
					hub := vmopv1.VirtualMachineClass{Spec: vmopv1.VirtualMachineClassSpec{Advanced: &vmopv1.VirtualMachineClassAdvancedSpec{PreferHTEnabled: vmopv1.FixedClassBool(true)}}}
					Expect(spoke.ConvertTo(&hub)).To(Succeed())
					Expect(hub.Spec.Advanced).To(BeNil())
					Expect(hub.Spec.ConfigSpec).To(MatchJSON(raw))
					Expect(version.newSpoke().ConvertFrom(&hub)).To(Succeed())
				}
				// Promoting an independent supported leaf must not make an
				// unsupported ExtraConfig shape fail the return projection.
				spoke := version.newSpoke()
				Expect(json.Unmarshal([]byte(`{"spec":{"configSpec":{"changeTrackingEnabled":true,"extraConfig":{"legacy":true}}}}`), spoke)).To(Succeed())
				var hub vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&hub)).To(Succeed())
				Expect(hub.Spec.Advanced.ChangeBlockTracking.Default).To(HaveValue(BeTrue()))
				Expect(hub.Spec.ConfigSpec).To(MatchJSON(`{"extraConfig":{"legacy":true}}`))
				Expect(version.newSpoke().ConvertFrom(&hub)).To(Succeed())
			})
			It("preserves unsupported duplicate NUMA members and null ExtraConfig values", func() {
				spoke := version.newSpoke()
				Expect(json.Unmarshal([]byte(`{"spec":{"configSpec":{"extraConfig":[{"key":"numa.nodeAffinity","value":"0,0"},{"key":"null.raw","value":null},{"key":"null.typed","value":{"_typeName":"string","_value":null}}]}}}`), spoke)).To(Succeed())
				hub := vmopv1.VirtualMachineClass{Spec: vmopv1.VirtualMachineClassSpec{Advanced: &vmopv1.VirtualMachineClassAdvancedSpec{PreferHTEnabled: &vmopv1.BoolPolicy{}}}}
				Expect(spoke.ConvertTo(&hub)).To(Succeed())
				Expect(hub.Spec.Advanced).To(BeNil())
				Expect(hub.Spec.ConfigSpec).To(MatchJSON(`{"_typeName":"VirtualMachineConfigSpec","extraConfig":[{"key":"numa.nodeAffinity","value":"0,0"},{"key":"null.raw","value":null},{"key":"null.typed","value":{"_typeName":"string","_value":null}}]}`))
			})
			It("overlays typed ExtraConfig by key without duplicates and retains unrelated raw properties", func() {
				f := false
				hub := classWithPolicies()
				hub.Spec.ConfigSpec = json.RawMessage(`{"annotation":"keep","extraConfig":[{"_typeName":"OptionValue","key":"numa.vcpu.preferHT","value":{"_typeName":"string","_value":"TRUE"},"future":42},{"key":"unrelated","value":{"_typeName":"string","_value":"keep"}}]}`)
				hub.Spec.Advanced = &vmopv1.VirtualMachineClassAdvancedSpec{PreferHTEnabled: &vmopv1.BoolPolicy{Default: &f}, ExtraConfig: &vmopv1.ExtraConfigListPolicy{Default: &[]common.KeyValuePair{{Key: "numa.vcpu.preferHT", Value: "TRUE"}}}}
				spoke := version.newSpoke()
				Expect(spoke.ConvertFrom(hub)).To(Succeed())
				encoded, err := json.Marshal(spoke)
				Expect(err).NotTo(HaveOccurred())
				var result struct {
					Spec struct {
						ConfigSpec struct {
							ExtraConfig []struct {
								Key   string
								Value struct {
									Value string `json:"_value"`
								}
								Future int
							}
						}
					}
				}
				Expect(json.Unmarshal(encoded, &result)).To(Succeed())
				Expect(result.Spec.ConfigSpec.ExtraConfig).To(HaveLen(2))
				count := 0
				for _, entry := range result.Spec.ConfigSpec.ExtraConfig {
					if entry.Key == "numa.vcpu.preferHT" {
						count++
						Expect(entry.Value.Value).To(Equal("FALSE"))
						Expect(entry.Future).To(Equal(42))
					}
				}
				Expect(count).To(Equal(1))
			})

			It("elevates advanced values while retaining unknown and non-string ExtraConfig", func() {
				spoke := version.newSpoke()
				Expect(json.Unmarshal([]byte(`{"spec":{"configSpec":{"_typeName":"VirtualMachineConfigSpec","changeTrackingEnabled":false,"futureField":{"n":9007199254740993},"extraConfig":[{"_typeName":"OptionValue","key":"numa.vcpu.preferHT","value":{"_typeName":"string","_value":"TRUE"}},{"key":"numa.nodeAffinity","value":"0,2"},{"key":"guestinfo.example","value":{"_typeName":"string","_value":"hello"}},{"key":"future.numeric","value":9007199254740993},{"key":"future.opaque","value":"hello","futureProperty":true}]}}}`), spoke)).To(Succeed())
				var hub vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&hub)).To(Succeed())
				Expect(hub.Spec.Advanced).NotTo(BeNil())
				Expect(hub.Spec.Advanced.ChangeBlockTracking.Default).To(HaveValue(BeFalse()))
				Expect(hub.Spec.Advanced.PreferHTEnabled.Default).To(HaveValue(BeTrue()))
				Expect(*hub.Spec.Advanced.PNUMANodeAffinity.Default).To(Equal([]int32{0, 2}))
				Expect((*hub.Spec.Advanced.ExtraConfig.Default)[0].Key).To(Equal("guestinfo.example"))
				Expect(hub.Spec.ConfigSpec).To(MatchJSON(`{"_typeName":"VirtualMachineConfigSpec","futureField":{"n":9007199254740993},"extraConfig":[{"key":"future.numeric","value":9007199254740993},{"key":"future.opaque","value":"hello","futureProperty":true}]}`))
			})
			It("preserves advanced policy metadata and honors one older field edit", func() {
				hub := classWithPolicies()
				f, t := false, true
				glob := "guestinfo.*"
				hub.Spec.Advanced = &vmopv1.VirtualMachineClassAdvancedSpec{
					ChangeBlockTracking: &vmopv1.BoolPolicy{Default: &t, AllowOverride: &t},
					PreferHTEnabled:     &vmopv1.BoolPolicy{Default: &f, AllowOverride: &t},
					ExtraConfig:         &vmopv1.ExtraConfigListPolicy{Allowed: &[]vmopv1.ExtraConfigMatcher{{Key: &vmopv1.StringMatcher{Glob: &glob}}}},
				}
				spoke := version.newSpoke()
				Expect(spoke.ConvertFrom(hub)).To(Succeed())
				var unchanged vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&unchanged)).To(Succeed())
				Expect(unchanged.Spec.Advanced).To(Equal(hub.Spec.Advanced))
				var obj map[string]interface{}
				encoded, err := json.Marshal(spoke)
				Expect(err).NotTo(HaveOccurred())
				Expect(json.Unmarshal(encoded, &obj)).To(Succeed())
				obj["spec"].(map[string]interface{})["configSpec"].(map[string]interface{})["changeTrackingEnabled"] = false
				encoded, err = json.Marshal(obj)
				Expect(err).NotTo(HaveOccurred())
				Expect(json.Unmarshal(encoded, spoke)).To(Succeed())
				var edited vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&edited)).To(Succeed())
				Expect(edited.Spec.Advanced.ChangeBlockTracking.Default).To(HaveValue(BeFalse()))
				Expect(edited.Spec.Advanced.ChangeBlockTracking.AllowOverride).To(HaveValue(BeFalse()))
				Expect(edited.Spec.Advanced.PreferHTEnabled).To(Equal(hub.Spec.Advanced.PreferHTEnabled))
				Expect(edited.Spec.Advanced.ExtraConfig).To(Equal(hub.Spec.Advanced.ExtraConfig))
			})

			It("promotes boot milliseconds and explicit false values while preserving unknown configuration", func() {
				spoke := version.newSpoke()
				Expect(json.Unmarshal([]byte(`{"spec":{"configSpec":{"_typeName":"VirtualMachineConfigSpec","annotation":"keep","bootOptions":{"_typeName":"VirtualMachineBootOptions","bootDelay":0,"bootRetryDelay":1500,"bootRetryEnabled":false,"efiSecureBootEnabled":true,"bootOrder":[]}}}}`), spoke)).To(Succeed())
				var hub vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&hub)).To(Succeed())
				Expect(*hub.Spec.BootOptions.BootDelay.Default).To(BeZero())
				Expect(*hub.Spec.BootOptions.BootRetryDelay.Default).To(Equal(int64(1500)))
				Expect(*hub.Spec.BootOptions.BootRetry.Default).To(BeFalse())
				Expect(*hub.Spec.BootOptions.EFISecureBoot.Default).To(BeTrue())
				Expect(hub.Spec.ConfigSpec).To(MatchJSON(`{"_typeName":"VirtualMachineConfigSpec","annotation":"keep","bootOptions":{"_typeName":"VirtualMachineBootOptions","bootOrder":[]}}`))
				projected := version.newSpoke()
				Expect(projected.ConvertFrom(&hub)).To(Succeed())
				var restored vmopv1.VirtualMachineClass
				Expect(projected.ConvertTo(&restored)).To(Succeed())
				Expect(apiequality.Semantic.DeepEqual(restored.Spec, hub.Spec)).To(BeTrue())
			})

			It("retains legacy boot delays outside the VM duration domain as raw presets", func() {
				spoke := version.newSpoke()
				Expect(json.Unmarshal([]byte(`{"spec":{"configSpec":{"_typeName":"VirtualMachineConfigSpec","bootOptions":{"bootDelay":-1,"bootRetryDelay":9223372036855}}}}`), spoke)).To(Succeed())
				var hub vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&hub)).To(Succeed())
				Expect(hub.Spec.BootOptions).To(BeNil())
				Expect(hub.Spec.ConfigSpec).To(MatchJSON(`{"_typeName":"VirtualMachineConfigSpec","bootOptions":{"bootDelay":-1,"bootRetryDelay":9223372036855}}`))
			})

			It("preserves boot policies per leaf while honoring older-writer raw edits", func() {
				hub := classWithPolicies()
				minimum, maximum, def := int64(0), int64(5000), int64(1000)
				enabled, allow := true, true
				hub.Spec.BootOptions = &vmopv1.VirtualMachineClassBootOptions{
					BootDelay:     &vmopv1.LongRange{Min: &minimum, Max: &maximum, Default: &def},
					EFISecureBoot: &vmopv1.BoolPolicy{Default: &enabled, AllowOverride: &allow},
				}
				spoke := version.newSpoke()
				Expect(spoke.ConvertFrom(hub)).To(Succeed())
				var restored vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&restored)).To(Succeed())
				Expect(restored.Spec.BootOptions).To(Equal(hub.Spec.BootOptions))
				data, err := json.Marshal(spoke)
				Expect(err).NotTo(HaveOccurred())
				var body map[string]any
				Expect(json.Unmarshal(data, &body)).To(Succeed())
				config := body["spec"].(map[string]any)["configSpec"].(map[string]any)
				config["bootOptions"].(map[string]any)["bootDelay"] = 2000
				data, err = json.Marshal(body)
				Expect(err).NotTo(HaveOccurred())
				Expect(json.Unmarshal(data, spoke)).To(Succeed())
				Expect(spoke.ConvertTo(&restored)).To(Succeed())
				Expect(*restored.Spec.BootOptions.BootDelay.Min).To(Equal(int64(2000)))
				Expect(*restored.Spec.BootOptions.BootDelay.Max).To(Equal(int64(2000)))
				Expect(restored.Spec.BootOptions.EFISecureBoot).To(Equal(hub.Spec.BootOptions.EFISecureBoot))
				delete(config, "bootOptions")
				data, err = json.Marshal(body)
				Expect(err).NotTo(HaveOccurred())
				Expect(json.Unmarshal(data, spoke)).To(Succeed())
				Expect(spoke.ConvertTo(&restored)).To(Succeed())
				Expect(restored.Spec.BootOptions).To(BeNil())
			})

			It("moves legacy scalars without changing units, devices, storage or reservation slots", func() {
				spoke := version.newSpoke()
				Expect(json.Unmarshal([]byte(`{
					"metadata":{"name":"legacy","namespace":"tenant"},
					"spec":{
						"hardware":{"cpus":4,"memory":"4Gi","devices":{
							"vgpuDevices":[{"profileName":"gpu-a"}],
							"dynamicDirectPathIODevices":[{"vendorID":1,"deviceID":2,"customLabel":"a"},{"vendorID":1,"deviceID":2,"customLabel":"b"}]},
							"instanceStorage":{"storageClass":"local","volumes":[{"size":"16Gi"}]}},
						"policies":{"resources":{"requests":{"cpu":"2","memory":"1Gi"},"limits":{"cpu":"4","memory":"8Gi"}}},
						"configSpec":{"_typeName":"VirtualMachineConfigSpec","annotation":"keep"},
						"reservedProfileID":"profile-a","reservedSlots":0
					}}`), spoke)).To(Succeed())
				before := spoke.DeepCopyObject()
				var hub vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&hub)).To(Succeed())
				Expect(hub.Spec.Resources.Size.CPU.Default.String()).To(Equal("4"))
				Expect(hub.Spec.Resources.Size.CPU.Min.Cmp(*hub.Spec.Resources.Size.CPU.Default)).To(BeZero())
				Expect(hub.Spec.Resources.Size.CPU.Max.Cmp(*hub.Spec.Resources.Size.CPU.Default)).To(BeZero())
				Expect(hub.Spec.Resources.Size.Memory.Default.String()).To(Equal("4Gi"))
				Expect(hub.Spec.Resources.Requests.CPU.Default.String()).To(Equal("2"))
				Expect(hub.Spec.Resources.Requests.Memory.Default.String()).To(Equal("1Gi"))
				Expect(hub.Spec.Resources.Limits.CPU.Default.String()).To(Equal("4"))
				Expect(hub.Spec.Resources.Limits.Memory.Default.String()).To(Equal("8Gi"))
				Expect(*hub.Spec.ReservedProfileName.Default).To(Equal("profile-a"))
				Expect(*hub.Spec.ReservedProfileName.Allowed).To(Equal([]string{"profile-a"}))
				Expect(*hub.Spec.ReservedProfileName.Required).To(BeTrue())
				Expect(hub.Spec.ReservedSlots).To(BeZero())
				Expect(hub.Spec.Hardware.Devices.VGPUDevices).To(Equal([]vmopv1.VGPUDevice{{ProfileName: "gpu-a"}}))
				Expect(hub.Spec.Hardware.Devices.DynamicDirectPathIODevices).To(Equal([]vmopv1.DynamicDirectPathIODevice{
					{VendorID: 1, DeviceID: 2, CustomLabel: "a"}, {VendorID: 1, DeviceID: 2, CustomLabel: "b"},
				}))
				Expect(hub.Spec.Hardware.InstanceStorage.StorageClass).To(Equal("local"))
				Expect(hub.Spec.Hardware.InstanceStorage.Volumes[0].Size.String()).To(Equal("16Gi"))
				Expect(hub.Spec.ConfigSpec).To(MatchJSON(`{"_typeName":"VirtualMachineConfigSpec","annotation":"keep"}`))
				Expect(apiequality.Semantic.DeepEqual(spoke, before)).To(BeTrue())
			})

			It("preserves policies through an unchanged older-client write", func() {
				hub := classWithPolicies()
				spoke := version.newSpoke()
				Expect(spoke.ConvertFrom(hub)).To(Succeed())
				before := spoke.DeepCopyObject()
				var restored vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&restored)).To(Succeed())
				Expect(restored.Spec).To(Equal(hub.Spec))
				Expect(restored.Annotations).NotTo(HaveKey(utilconversion.AnnotationKey))
				Expect(apiequality.Semantic.DeepEqual(spoke, before)).To(BeTrue())
				Expect(hub.Annotations).NotTo(HaveKey(utilconversion.AnnotationKey))
			})

			It("promotes supported compute fields using effective legacy size and preserves unknown raw values", func() {
				spoke := version.newSpoke()
				Expect(json.Unmarshal([]byte(`{"spec":{"hardware":{"cpus":8,"memory":"4Gi"},"configSpec":{"_typeName":"VirtualMachineConfigSpec","numCPUs":16,"numCoresPerSocket":2,"virtualNuma":{"coresPerNumaNode":4,"exposeVnumaOnCpuHotadd":false},"cpuHotRemoveEnabled":true,"latencySensitivity":{"level":"high"},"simultaneousThreads":2,"flags":{"vvtdEnabled":false,"future":{"value":12345678901234567890}}}}}`), spoke)).To(Succeed())
				var hub vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&hub)).To(Succeed())
				Expect(*hub.Spec.CPUAdvanced.Topology.VNUMANodeCount.Default).To(Equal(int32(2)))
				Expect(*hub.Spec.CPUAdvanced.HotRemoveEnabled.Default).To(BeTrue())
				Expect(*hub.Spec.CPUAdvanced.IOMMUEnabled.Default).To(BeFalse())
				Expect(*hub.Spec.CPUAdvanced.LatencySensitivity.Default).To(Equal("HighWithHyperthreading"))
				Expect(hub.Spec.ConfigSpec).To(MatchJSON(`{"_typeName":"VirtualMachineConfigSpec","numCPUs":16,"virtualNuma":{"exposeVnumaOnCpuHotadd":false},"flags":{"future":{"value":12345678901234567890}}}`))
			})

			It("preserves compute policies and honors individual older-writer edits", func() {
				hub := classWithPolicies()
				value, override := true, true
				hub.Spec.CPUAdvanced = &vmopv1.VirtualMachineClassCPUAdvancedSpec{
					HotRemoveEnabled: &vmopv1.BoolPolicy{Default: &value, AllowOverride: &override},
					IOMMUEnabled:     &vmopv1.BoolPolicy{Default: &value, AllowOverride: &override},
				}
				spoke := version.newSpoke()
				Expect(spoke.ConvertFrom(hub)).To(Succeed())
				var restored vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&restored)).To(Succeed())
				Expect(restored.Spec).To(Equal(hub.Spec))
				body, err := json.Marshal(spoke)
				Expect(err).NotTo(HaveOccurred())
				var old struct {
					Spec struct {
						ConfigSpec json.RawMessage `json:"configSpec"`
					} `json:"spec"`
				}
				Expect(json.Unmarshal(body, &old)).To(Succeed())
				doc, err := vmopv1.ParseClassConfigSpec(old.Spec.ConfigSpec)
				Expect(err).NotTo(HaveOccurred())
				Expect(doc.Set("cpuHotRemoveEnabled", false)).To(Succeed())
				old.Spec.ConfigSpec, err = doc.Marshal()
				Expect(err).NotTo(HaveOccurred())
				body, err = json.Marshal(old)
				Expect(err).NotTo(HaveOccurred())
				Expect(json.Unmarshal(body, spoke)).To(Succeed())
				Expect(spoke.ConvertTo(&restored)).To(Succeed())
				Expect(restored.Spec.CPUAdvanced.HotRemoveEnabled).To(Equal(vmopv1.FixedClassBool(false)))
				Expect(restored.Spec.CPUAdvanced.IOMMUEnabled).To(Equal(hub.Spec.CPUAdvanced.IOMMUEnabled))
				Expect(restored.Spec.Resources).To(Equal(hub.Spec.Resources))
			})

			It("retains unsupported raw compute values and clears stale reused destinations", func() {
				spoke := version.newSpoke()
				Expect(json.Unmarshal([]byte(`{"spec":{"hardware":{"cpus":8},"configSpec":{"latencySensitivity":{"level":"low"},"virtualNuma":{"coresPerNumaNode":3}}}}`), spoke)).To(Succeed())
				hub := vmopv1.VirtualMachineClass{Spec: vmopv1.VirtualMachineClassSpec{CPUAdvanced: &vmopv1.VirtualMachineClassCPUAdvancedSpec{HotRemoveEnabled: vmopv1.FixedClassBool(true)}}}
				Expect(spoke.ConvertTo(&hub)).To(Succeed())
				Expect(hub.Spec.CPUAdvanced).To(BeNil())
				Expect(hub.Spec.ConfigSpec).To(MatchJSON(`{"latencySensitivity":{"level":"low"},"virtualNuma":{"coresPerNumaNode":3}}`))
			})

			It("retains unrepresentable latency and SMT combinations unchanged", func() {
				for _, pair := range []struct {
					level   string
					threads int32
				}{{"high", 0}, {"high", 3}, {"normal", 2}} {
					spoke := version.newSpoke()
					body, err := json.Marshal(map[string]any{"spec": map[string]any{"configSpec": map[string]any{"latencySensitivity": map[string]any{"level": pair.level}, "simultaneousThreads": pair.threads}}})
					Expect(err).NotTo(HaveOccurred())
					Expect(json.Unmarshal(body, spoke)).To(Succeed())
					var hub vmopv1.VirtualMachineClass
					Expect(spoke.ConvertTo(&hub)).To(Succeed())
					Expect(hub.Spec.CPUAdvanced).To(BeNil())
					raw, err := vmopv1.ProjectClassConfigSpec(&hub.Spec)
					Expect(err).NotTo(HaveOccurred())
					doc, err := vmopv1.ParseClassConfigSpec(raw)
					Expect(err).NotTo(HaveOccurred())
					var threads int32
					ok, err := doc.Get("simultaneousThreads", &threads)
					Expect(err).NotTo(HaveOccurred())
					Expect(ok).To(BeTrue())
					Expect(threads).To(Equal(pair.threads))
				}
			})
			It("preserves unrepresentable raw leaf types and incompatible NUMA topology", func() {
				spoke := version.newSpoke()
				raw := `{"cpuHotRemoveEnabled":"legacy-invalid","numCoresPerSocket":1.5,"latencySensitivity":{"level":1}}`
				Expect(json.Unmarshal([]byte(`{"spec":{"configSpec":`+raw+`}}`), spoke)).To(Succeed())
				var hub vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&hub)).To(Succeed())
				Expect(hub.Spec.CPUAdvanced).To(BeNil())
				Expect(hub.Spec.ConfigSpec).To(MatchJSON(raw))
				Expect(json.Unmarshal([]byte(`{"spec":{"hardware":{"cpus":6},"configSpec":{"numCoresPerSocket":2,"virtualNuma":{"coresPerNumaNode":3}}}}`), spoke)).To(Succeed())
				Expect(spoke.ConvertTo(&hub)).To(Succeed())
				Expect(hub.Spec.CPUAdvanced.Topology.VNUMANodeCount).To(BeNil())
				Expect(hub.Spec.ConfigSpec).To(MatchJSON(`{"virtualNuma":{"coresPerNumaNode":3}}`))
			})
			It("copies metadata before preserving class policies", func() {
				hub := classWithPolicies()
				hub.Annotations = map[string]string{"user": "keep"}
				hub.Labels = map[string]string{"user": "keep"}
				before := hub.DeepCopy()
				spoke := version.newSpoke()
				Expect(spoke.ConvertFrom(hub)).To(Succeed())
				Expect(hub).To(Equal(before))
				spoke.(metav1.Object).GetLabels()["user"] = "changed"
				spoke.(metav1.Object).GetAnnotations()["user"] = "changed"
				Expect(hub).To(Equal(before))
			})

			It("honors older-writer edits without discarding other policies", func() {
				hub := classWithPolicies()
				spoke := version.newSpoke()
				Expect(spoke.ConvertFrom(hub)).To(Succeed())
				Expect(json.Unmarshal([]byte(`{"spec":{"hardware":{"cpus":8},"reservedProfileID":"profile-b","reservedSlots":0}}`), spoke)).To(Succeed())
				var restored vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&restored)).To(Succeed())
				Expect(restored.Spec.Resources.Size.CPU.Default.String()).To(Equal("8"))
				Expect(restored.Spec.Resources.Size.CPU.Min.String()).To(Equal("8"))
				Expect(restored.Spec.Resources.Size.CPU.Max.String()).To(Equal("8"))
				Expect(restored.Spec.Resources.Size.Memory).To(Equal(hub.Spec.Resources.Size.Memory))
				Expect(restored.Spec.Hardware.Devices).To(Equal(hub.Spec.Hardware.Devices))
				Expect(*restored.Spec.ReservedProfileName.Default).To(Equal("profile-b"))
				Expect(*restored.Spec.ReservedProfileName.Allowed).To(Equal([]string{"profile-b"}))
				Expect(*restored.Spec.ReservedProfileName.Required).To(BeTrue())
				Expect(restored.Spec.ReservedSlots).To(BeZero())
			})

			DescribeTable("preserves profile selection states through older clients",
				func(policy *vmopv1.EnumPolicy) {
					hub := classWithPolicies()
					hub.Spec.ReservedProfileName = policy
					spoke := version.newSpoke()
					Expect(spoke.ConvertFrom(hub)).To(Succeed())
					var restored vmopv1.VirtualMachineClass
					Expect(spoke.ConvertTo(&restored)).To(Succeed())
					Expect(restored.Spec).To(Equal(hub.Spec))
				},
				Entry("omitted policy", (*vmopv1.EnumPolicy)(nil)),
				Entry("empty allowed list", &vmopv1.EnumPolicy{Allowed: &[]string{}}),
				Entry("implicit single allowed profile", &vmopv1.EnumPolicy{Allowed: &[]string{"profile-a"}}),
			)

			It("honors an older writer clearing the profile without losing resource ranges", func() {
				hub := classWithPolicies()
				spoke := version.newSpoke()
				Expect(spoke.ConvertFrom(hub)).To(Succeed())
				Expect(json.Unmarshal([]byte(`{"spec":{"reservedProfileID":""}}`), spoke)).To(Succeed())
				var restored vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&restored)).To(Succeed())
				Expect(restored.Spec.ReservedProfileName).To(BeNil())
				expected := hub.Spec.DeepCopy()
				expected.ReservedProfileName = nil
				Expect(restored.Spec).To(Equal(*expected))
			})

			It("preserves nil, explicit zero and empty list policy values", func() {
				zero := resource.MustParse("0")
				empty := []vmopv1.VGPUDevice{}
				hub := &vmopv1.VirtualMachineClass{Spec: vmopv1.VirtualMachineClassSpec{
					Resources: vmopv1.VirtualMachineClassResources{Requests: vmopv1.VirtualMachineClassResourceSpec{CPU: &vmopv1.ResourceQuantityRange{Default: &zero}}},
					Hardware:  vmopv1.VirtualMachineClassHardware{Devices: vmopv1.VirtualDevices{VGPUDevices: empty}},
				}}
				spoke := version.newSpoke()
				Expect(spoke.ConvertFrom(hub)).To(Succeed())
				var restored vmopv1.VirtualMachineClass
				Expect(spoke.ConvertTo(&restored)).To(Succeed())
				Expect(restored.Spec.Resources.Size.CPU).To(BeNil())
				Expect(restored.Spec.Resources.Requests.CPU.Default).NotTo(BeNil())
				Expect(restored.Spec.Resources.Requests.CPU.Default.IsZero()).To(BeTrue())
				Expect(restored.Spec.Resources.Requests.CPU.Min).To(BeNil())
				Expect(restored.Spec.Hardware.Devices.VGPUDevices).NotTo(BeNil())
				Expect(restored.Spec.Hardware.Devices.VGPUDevices).To(BeEmpty())
			})

			It("returns malformed conversion data without modifying the input", func() {
				spoke := version.newSpoke()
				spoke.(metav1.Object).SetAnnotations(map[string]string{utilconversion.AnnotationKey: "{"})
				before := spoke.DeepCopyObject()
				Expect(spoke.ConvertTo(&vmopv1.VirtualMachineClass{})).NotTo(Succeed())
				Expect(apiequality.Semantic.DeepEqual(spoke, before)).To(BeTrue())
			})
		})
	}

	It("preserves policies in client-side scheme and list conversion", func() {
		scheme := runtime.NewScheme()
		Expect(vmopv1a5.AddToScheme(scheme)).To(Succeed())
		Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
		hub := &vmopv1.VirtualMachineClassList{Items: []vmopv1.VirtualMachineClass{*classWithPolicies()}}
		var spoke vmopv1a5.VirtualMachineClassList
		Expect(scheme.Convert(hub, &spoke, nil)).To(Succeed())
		var restored vmopv1.VirtualMachineClassList
		Expect(scheme.Convert(&spoke, &restored, nil)).To(Succeed())
		Expect(restored.Items[0].Spec).To(Equal(hub.Items[0].Spec))
	})

	for _, version := range []struct {
		name     string
		newSpoke func() ctrlconversion.Convertible
	}{
		{"v1alpha4", func() ctrlconversion.Convertible { return &vmopv1a4.VirtualMachineClassInstance{} }},
		{"v1alpha5", func() ctrlconversion.Convertible { return &vmopv1a5.VirtualMachineClassInstance{} }},
	} {
		Context(version.name+" class instances", func() {
			It("preserves policies and isolates source metadata in both directions", func() {
				hub := &vmopv1.VirtualMachineClassInstance{
					ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"user": "keep"}, Labels: map[string]string{"user": "keep"}},
					Spec:       vmopv1.VirtualMachineClassInstanceSpec{VirtualMachineClassSpec: classWithPolicies().Spec},
				}
				beforeHub := hub.DeepCopy()
				spoke := version.newSpoke()
				Expect(spoke.ConvertFrom(hub)).To(Succeed())
				Expect(hub).To(Equal(beforeHub))
				beforeSpoke := spoke.DeepCopyObject()
				var restored vmopv1.VirtualMachineClassInstance
				Expect(spoke.ConvertTo(&restored)).To(Succeed())
				Expect(restored.Spec).To(Equal(hub.Spec))
				Expect(restored.Annotations).NotTo(HaveKey(utilconversion.AnnotationKey))
				restored.Labels["user"] = "changed"
				restored.Annotations["user"] = "changed"
				Expect(spoke).To(Equal(beforeSpoke))
				spoke.(metav1.Object).GetLabels()["user"] = "changed"
				spoke.(metav1.Object).GetAnnotations()["user"] = "changed"
				Expect(hub).To(Equal(beforeHub))
			})
			It("returns malformed annotations without changing source metadata", func() {
				spoke := version.newSpoke()
				spoke.(metav1.Object).SetAnnotations(map[string]string{utilconversion.AnnotationKey: "{", "user": "keep"})
				before := spoke.DeepCopyObject()
				Expect(spoke.ConvertTo(&vmopv1.VirtualMachineClassInstance{})).To(MatchError(ContainSubstring("restore VMClassInstance conversion data")))
				Expect(spoke).To(Equal(before))
			})
		})
	}

})

var _ = Describe("VMClass allocation conversions", Label("api"), func() {
	for _, version := range []struct {
		name         string
		newResources func() any
	}{
		{"v1alpha1", func() any { return &vmopv1a1.VirtualMachineClassResources{} }},
		{"v1alpha2", func() any { return &vmopv1a2.VirtualMachineClassResources{} }},
		{"v1alpha3", func() any { return &vmopv1a3.VirtualMachineClassResources{} }},
		{"v1alpha4", func() any { return &vmopv1a4.VirtualMachineClassResources{} }},
		{"v1alpha5", func() any { return &vmopv1a5.VirtualMachineClassResources{} }},
	} {
		Context(version.name, func() {
			It("updates allocations without replacing size and isolates quantity storage", func() {
				scheme := runtime.NewScheme()
				Expect(vmopv1a1.AddToScheme(scheme)).To(Succeed())
				Expect(vmopv1a2.AddToScheme(scheme)).To(Succeed())
				Expect(vmopv1a3.AddToScheme(scheme)).To(Succeed())
				Expect(vmopv1a4.AddToScheme(scheme)).To(Succeed())
				Expect(vmopv1a5.AddToScheme(scheme)).To(Succeed())
				Expect(vmopv1.AddToScheme(scheme)).To(Succeed())
				spoke := version.newResources()
				Expect(json.Unmarshal([]byte(`{"requests":{"cpu":"123456789012345678901234567890","memory":"1Gi"},"limits":{"cpu":"4","memory":"8Gi"}}`), spoke)).To(Succeed())
				before, err := json.Marshal(spoke)
				Expect(err).NotTo(HaveOccurred())
				size := vmopv1.FixedClassResourcePair(resource.MustParse("7"), resource.MustParse("9Gi"))
				hub := vmopv1.VirtualMachineClassResources{Size: *size.DeepCopy()}
				Expect(scheme.Convert(spoke, &hub, nil)).To(Succeed())
				Expect(hub.Size).To(Equal(size))
				originalCPU := resource.MustParse("123456789012345678901234567890")
				Expect(hub.Requests.CPU.Default.Cmp(originalCPU)).To(BeZero())
				Expect(hub.Requests.Memory.Default.String()).To(Equal("1Gi"))
				Expect(hub.Limits.CPU.Default.String()).To(Equal("4"))
				Expect(hub.Limits.Memory.Default.String()).To(Equal("8Gi"))

				projected := version.newResources()
				Expect(scheme.Convert(&hub, projected, nil)).To(Succeed())
				actual, err := json.Marshal(projected)
				Expect(err).NotTo(HaveOccurred())
				Expect(actual).To(MatchJSON(before))
				hub.Requests.CPU.Default.Add(resource.MustParse("1"))
				Expect(hub.Requests.CPU.Min.Cmp(originalCPU)).To(BeZero())
				Expect(hub.Requests.CPU.Max.Cmp(originalCPU)).To(BeZero())
				var fromOriginal, fromProjected vmopv1.VirtualMachineClassResources
				Expect(scheme.Convert(spoke, &fromOriginal, nil)).To(Succeed())
				Expect(scheme.Convert(projected, &fromProjected, nil)).To(Succeed())
				Expect(fromOriginal.Requests.CPU.Default.Cmp(originalCPU)).To(BeZero())
				Expect(fromProjected.Requests.CPU.Default.Cmp(originalCPU)).To(BeZero())
				actual, err = json.Marshal(spoke)
				Expect(err).NotTo(HaveOccurred())
				Expect(actual).To(MatchJSON(before))
				actual, err = json.Marshal(projected)
				Expect(err).NotTo(HaveOccurred())
				Expect(actual).To(MatchJSON(before))
			})
		})
	}
})

func classWithPolicies() *vmopv1.VirtualMachineClass {
	minimum, maximum, def := resource.MustParse("2"), resource.MustParse("16"), resource.MustParse("4")
	memory := resource.MustParse("4Gi")
	profile := "profile-a"
	profiles := []string{"profile-a", "profile-b"}
	required := true
	devices := []vmopv1.VGPUDevice{{ProfileName: "gpu-a"}}
	return &vmopv1.VirtualMachineClass{
		ObjectMeta: metav1.ObjectMeta{Name: "class", Namespace: "tenant"},
		Spec: vmopv1.VirtualMachineClassSpec{
			Resources: vmopv1.VirtualMachineClassResources{Size: vmopv1.VirtualMachineClassResourceSpec{
				CPU:    &vmopv1.ResourceQuantityRange{Min: &minimum, Max: &maximum, Default: &def},
				Memory: &vmopv1.ResourceQuantityRange{Default: &memory},
			}},
			Hardware:            vmopv1.VirtualMachineClassHardware{Devices: vmopv1.VirtualDevices{VGPUDevices: devices}},
			ReservedProfileName: &vmopv1.EnumPolicy{Default: &profile, Allowed: &profiles, Required: &required},
			ReservedSlots:       4,
		},
	}
}

var _ = Describe("Shared class preset helpers", Label("api"), func() {
	It("edits nested presets without losing unknown objects or integer precision", func() {
		raw := json.RawMessage(`{"_typeName":"VirtualMachineConfigSpec","flags":{"vvtdEnabled":false,"future":{"number":123456789012345678901234567890}},"cpuHotAddEnabled":true}`)
		before := append(json.RawMessage(nil), raw...)
		doc, err := vmopv1.ParseClassConfigSpec(raw)
		Expect(err).NotTo(HaveOccurred())
		var value bool
		ok, err := doc.Get("flags.vvtdEnabled", &value)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(value).To(BeFalse())
		untouched, err := doc.Marshal()
		Expect(err).NotTo(HaveOccurred())
		Expect(untouched).To(Equal(raw))
		Expect(doc.Delete("flags.vvtdEnabled")).To(Succeed())
		Expect(doc.Set("bootOptions.bootDelay", int64(1234567890123456789))).To(Succeed())
		changed, err := doc.Marshal()
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(MatchJSON(`{"_typeName":"VirtualMachineConfigSpec","flags":{"future":{"number":123456789012345678901234567890}},"cpuHotAddEnabled":true,"bootOptions":{"bootDelay":1234567890123456789}}`))
		Expect(raw).To(Equal(before))
	})
	It("preserves nil and explicit false defaults independently", func() {
		Expect(vmopv1.ClassBoolDefault(nil)).To(BeNil())
		policy := vmopv1.FixedClassBool(false)
		Expect(*policy.AllowOverride).To(BeFalse())
		projected := vmopv1.ClassBoolDefault(policy)
		Expect(projected).NotTo(BeNil())
		Expect(*projected).To(BeFalse())
		*projected = true
		Expect(*policy.Default).To(BeFalse())
		def := ""
		allowed := []string{"fallback"}
		Expect(*vmopv1.ClassEnumDefault(&vmopv1.EnumPolicy{Default: &def, Allowed: &allowed})).To(BeEmpty())
		Expect(*vmopv1.ClassEnumDefault(&vmopv1.EnumPolicy{Allowed: &allowed})).To(Equal("fallback"))
	})
	It("distinguishes null values from explicit false and reports malformed objects", func() {
		doc, err := vmopv1.ParseClassConfigSpec(json.RawMessage(`{"flags":null,"cpuHotAddEnabled":null}`))
		Expect(err).NotTo(HaveOccurred())
		var value bool
		ok, err := doc.Get("cpuHotAddEnabled", &value)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
		ok, err = doc.Get("flags.vvtdEnabled", &value)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeFalse())
		_, err = vmopv1.ParseClassConfigSpec(json.RawMessage(`{"flags":`))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("Typed compute projection precedence", Label("api"), func() {
	It("overrides conflicting raw hyperthreading for a High typed default", func() {
		level := "High"
		spec := vmopv1.VirtualMachineClassSpec{
			ConfigSpec:  json.RawMessage(`{"latencySensitivity":{"level":"high"},"simultaneousThreads":2}`),
			CPUAdvanced: &vmopv1.VirtualMachineClassCPUAdvancedSpec{LatencySensitivity: &vmopv1.EnumPolicy{Default: &level}},
		}
		raw, err := vmopv1.ProjectClassConfigSpec(&spec)
		Expect(err).NotTo(HaveOccurred())
		doc, err := vmopv1.ParseClassConfigSpec(raw)
		Expect(err).NotTo(HaveOccurred())
		var threads int32
		ok, err := doc.Get("simultaneousThreads", &threads)
		Expect(err).NotTo(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(threads).To(Equal(int32(1)))
	})
})

var _ = Describe("CPU VM counterpart conversion", Label("api"), func() {
	for _, version := range []struct {
		name  string
		spoke func() ctrlconversion.Convertible
	}{
		{"v1alpha1", func() ctrlconversion.Convertible { return &vmopv1a1.VirtualMachine{} }},
		{"v1alpha2", func() ctrlconversion.Convertible { return &vmopv1a2.VirtualMachine{} }},
		{"v1alpha3", func() ctrlconversion.Convertible { return &vmopv1a3.VirtualMachine{} }},
		{"v1alpha4", func() ctrlconversion.Convertible { return &vmopv1a4.VirtualMachine{} }},
		{"v1alpha5", func() ctrlconversion.Convertible { return &vmopv1a5.VirtualMachine{} }},
	} {
		It("preserves new flags through "+version.name, func() {
			value := true
			hub := &vmopv1.VirtualMachine{Spec: vmopv1.VirtualMachineSpec{CPUAdvanced: &vmopv1.VirtualMachineCPUAdvancedSpec{HotRemoveEnabled: &value}}}
			spoke := version.spoke()
			Expect(spoke.ConvertFrom(hub)).To(Succeed())
			var restored vmopv1.VirtualMachine
			Expect(spoke.ConvertTo(&restored)).To(Succeed())
			Expect(restored.Spec.CPUAdvanced.HotRemoveEnabled).NotTo(BeNil())
			Expect(*restored.Spec.CPUAdvanced.HotRemoveEnabled).To(BeTrue())
		})
	}
})

var _ = Describe("Indexed class ExtraConfig overlay", Label("api"), func() {
	It("merges many keys once while preserving residual duplicates and typed precedence", func() {
		entries := []json.RawMessage{
			json.RawMessage(`{"key":"unmatched","value":"first"}`),
			json.RawMessage(`{"key":"unmatched","value":"second"}`),
			json.RawMessage(`null`),
			json.RawMessage(`"unsupported"`),
		}
		values := []common.KeyValuePair{}
		for i := 0; i < 512; i++ {
			key := fmt.Sprintf("guestinfo.key%d", i)
			values = append(values, common.KeyValuePair{Key: key, Value: fmt.Sprintf("typed%d", i)})
			if i < 256 {
				raw, err := json.Marshal(map[string]any{"key": key, "value": "raw", "future": i})
				Expect(err).NotTo(HaveOccurred())
				entries = append(entries, raw)
			}
		}
		entries = append(entries, json.RawMessage(`{"key":"guestinfo.key0","value":"duplicate","anotherFuture":42}`))
		values = append(values, common.KeyValuePair{Key: "guestinfo.key0", Value: "last"}, common.KeyValuePair{Key: "numa.vcpu.preferHT", Value: "TRUE"})
		raw, err := json.Marshal(map[string]any{"extraConfig": entries})
		Expect(err).NotTo(HaveOccurred())
		spec := vmopv1.VirtualMachineClassSpec{
			ConfigSpec: raw,
			Advanced: &vmopv1.VirtualMachineClassAdvancedSpec{
				ExtraConfig:     &vmopv1.ExtraConfigListPolicy{Default: &values},
				PreferHTEnabled: vmopv1.FixedClassBool(false),
			},
		}
		projected, err := vmopv1.ProjectClassConfigSpec(&spec)
		Expect(err).NotTo(HaveOccurred())
		doc, err := vmopv1.ParseClassConfigSpec(projected)
		Expect(err).NotTo(HaveOccurred())
		var actual []json.RawMessage
		present, err := doc.Get("extraConfig", &actual)
		Expect(err).NotTo(HaveOccurred())
		Expect(present).To(BeTrue())
		Expect(actual).To(HaveLen(517))
		for i := 0; i < 4; i++ {
			Expect(actual[i]).To(MatchJSON(entries[i]))
		}
		for i, raw := range actual[4:] {
			var item struct {
				Key   string
				Value struct {
					Value string `json:"_value"`
				}
				Future        *int
				AnotherFuture *int
			}
			Expect(json.Unmarshal(raw, &item)).To(Succeed())
			switch {
			case i < 511:
				n := i + 1
				Expect(item.Key).To(Equal(fmt.Sprintf("guestinfo.key%d", n)))
				Expect(item.Value.Value).To(Equal(fmt.Sprintf("typed%d", n)))
				if n < 256 {
					Expect(item.Future).To(HaveValue(Equal(n)))
				}
			case i == 511:
				Expect(item.Key).To(Equal("guestinfo.key0"))
				Expect(item.Value.Value).To(Equal("last"))
				Expect(item.Future).To(HaveValue(Equal(0)))
				Expect(item.AnotherFuture).To(HaveValue(Equal(42)))
			default:
				Expect(item.Key).To(Equal("numa.vcpu.preferHT"))
				Expect(item.Value.Value).To(Equal("FALSE"))
			}
		}
	})
})
