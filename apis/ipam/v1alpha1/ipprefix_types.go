/*
Copyright 2022 Nokia.

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

package v1alpha1

import (
	"reflect"

	"github.com/nokia/k8s-ipam/internal/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// IPPrefixSpec defines the desired state of IPPrefix
type IPPrefixSpec struct {
	// +kubebuilder:validation:Enum=`network`;`loopback`;`pool`;`aggregate`
	// +kubebuilder:default=network
	PrefixKind PrefixKind `json:"kind" yaml:"kind"`
	// NetworkInstance identifies the network instance the IP prefix belongs to
	NetworkInstance string `json:"networkInstance" yaml:"networkInstance"`
	// Prefix defines the ip subnet of the ip prefix, it can also be an address if a /32 or /128 is specified
	// +kubebuilder:validation:Pattern=`(([0-9]|[1-9][0-9]|1[0-9][0-9]|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9][0-9]|2[0-4][0-9]|25[0-5])/(([0-9])|([1-2][0-9])|(3[0-2]))|((:|[0-9a-fA-F]{0,4}):)([0-9a-fA-F]{0,4}:){0,5}((([0-9a-fA-F]{0,4}:)?(:|[0-9a-fA-F]{0,4}))|(((25[0-5]|2[0-4][0-9]|[01]?[0-9]?[0-9])\.){3}(25[0-5]|2[0-4][0-9]|[01]?[0-9]?[0-9])))(/(([0-9])|([0-9]{2})|(1[0-1][0-9])|(12[0-8])))`
	Prefix string `json:"prefix" yaml:"prefix"`
	// Labels provide metadata to the prefix. They are part of the spec since the allocation
	// selector will use these labels for allocation more specific prefixes/addresses within this prefix
	// As such we distinguish clearly between the metadata labels and the labels used in the spec
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// IPPrefixStatus defines the observed state of IPPrefix
type IPPrefixStatus struct {
	ConditionedStatus `json:",inline" yaml:",inline"`
	// AllocatedPrefix identifies the prefix that was allocated by the IPAM system
	AllocatedPrefix string `json:"prefix,omitempty" yaml:"prefix,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="SYNC",type="string",JSONPath=".status.conditions[?(@.kind=='Synced')].status"
// +kubebuilder:printcolumn:name="STATUS",type="string",JSONPath=".status.conditions[?(@.kind=='Ready')].status"
// +kubebuilder:printcolumn:name="NETWORK-INSTANCE",type="string",JSONPath=".spec.networkInstance"
// +kubebuilder:printcolumn:name="KIND",type="string",JSONPath=".spec.kind"
// +kubebuilder:printcolumn:name="SUBNET",type="string",JSONPath=".spec.subnetName"
// +kubebuilder:printcolumn:name="PREFIX-REQ",type="string",JSONPath=".spec.prefix"
// +kubebuilder:printcolumn:name="PREFIX-ALLOC",type="string",JSONPath=".status.prefix"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:categories={nephio,ipam}

// IPPrefix is the Schema for the ipprefixes API
type IPPrefix struct {
	metav1.TypeMeta   `json:",inline" yaml:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`

	Spec   IPPrefixSpec   `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status IPPrefixStatus `json:"status,omitempty" yaml:"status,omitempty"`
}

//+kubebuilder:object:root=true

// IPPrefixList contains a list of IPPrefix
type IPPrefixList struct {
	metav1.TypeMeta `json:",inline" yaml:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Items           []IPPrefix `json:"items" yaml:"items"`
}

func init() {
	SchemeBuilder.Register(&IPPrefix{}, &IPPrefixList{})
}

var (
	IPPrefixKind             = reflect.TypeOf(IPPrefix{}).Name()
	IPPrefixGroupKind        = schema.GroupKind{Group: GroupVersion.Group, Kind: IPPrefixKind}.String()
	IPPrefixKindAPIVersion   = IPPrefixKind + "." + GroupVersion.String()
	IPPrefixGroupVersionKind = GroupVersion.WithKind(IPPrefixKind)
	IPPrefixKindGVKString    = meta.GVKToString(&schema.GroupVersionKind{
		Group:   GroupVersion.Group,
		Version: GroupVersion.Version,
		Kind:    IPPrefixKind,
	})
)
