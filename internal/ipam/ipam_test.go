package ipam

import (
	"context"
	"fmt"
	"strings"
	"testing"

	ipamv1alpha1 "github.com/nokia/k8s-ipam/apis/ipam/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type allocation struct {
	kind      string
	name      string
	namespace string
	spec      ipamv1alpha1.IPAllocationSpec
}

func buildNetworkInstance(alloc *allocation) *ipamv1alpha1.NetworkInstance {
	return &ipamv1alpha1.NetworkInstance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: ipamv1alpha1.GroupVersion.String(),
			Kind:       ipamv1alpha1.NetworkInstanceKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: alloc.namespace,
			Name:      alloc.name,
		},
	}
}

func buildIPAllocation(alloc *allocation) *ipamv1alpha1.IPAllocation {
	switch alloc.kind {
	case ipamv1alpha1.NetworkInstanceKind:
		return ipamv1alpha1.BuildIPAllocationFromNetworkInstancePrefix(
			&ipamv1alpha1.NetworkInstance{
				TypeMeta: metav1.TypeMeta{
					APIVersion: ipamv1alpha1.GroupVersion.String(),
					Kind:       ipamv1alpha1.NetworkInstanceKind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: alloc.namespace,
					Name:      alloc.name,
				},
			},
			&ipamv1alpha1.Prefix{
				Prefix: alloc.spec.Prefix,
				Labels: alloc.spec.Labels,
			},
		)
	case ipamv1alpha1.IPPrefixKind:
		return ipamv1alpha1.BuildIPAllocationFromIPPrefix(
			&ipamv1alpha1.IPPrefix{
				TypeMeta: metav1.TypeMeta{
					APIVersion: ipamv1alpha1.GroupVersion.String(),
					Kind:       ipamv1alpha1.IPPrefixKind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: alloc.namespace,
					Name:      alloc.name,
				},
				Spec: ipamv1alpha1.IPPrefixSpec{
					NetworkInstance: alloc.spec.NetworkInstance,
					PrefixKind:      alloc.spec.PrefixKind,
					Prefix:          alloc.spec.Prefix,
					Labels:          alloc.spec.Labels,
				},
			},
		)
	case ipamv1alpha1.IPAllocationKind:
		return ipamv1alpha1.BuildIPAllocationFromIPAllocation(
			&ipamv1alpha1.IPAllocation{
				TypeMeta: metav1.TypeMeta{
					APIVersion: ipamv1alpha1.GroupVersion.String(),
					Kind:       ipamv1alpha1.IPAllocationKind,
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: alloc.namespace,
					Name:      alloc.name,
				},
				Spec: alloc.spec,
			},
		)
	}
	return nil
}

func TestNetworkInstance(t *testing.T) {
	namespace := "dummy"
	niName := "niName"
	niCreate := &allocation{namespace: namespace, name: niName}
	niPrefixAlloc := &allocation{
		kind:      ipamv1alpha1.NetworkInstanceKind,
		namespace: namespace,
		name:      niName,
		spec: ipamv1alpha1.IPAllocationSpec{
			NetworkInstance: niName,
			Prefix:          "10.0.0.0/8",
		},
	}

	net1PrefixAllocWoCreate := &allocation{
		kind:      ipamv1alpha1.IPAllocationKind,
		namespace: namespace,
		name:      "alloc-net1-prefix1",
		spec: ipamv1alpha1.IPAllocationSpec{
			NetworkInstance: niName,
			PrefixKind:      ipamv1alpha1.PrefixKindNetwork,
			Prefix:          "10.0.0.2/24",
			Labels: map[string]string{
				"nephio.org/gateway":      "true",
				"nephio.org/region":       "us-central1",
				"nephio.org/site":         "edge1",
				"nephio.org/network-name": "net1",
			},
		},
	}

	net1PrefixAllocCreate := &allocation{
		kind:      ipamv1alpha1.IPAllocationKind,
		namespace: namespace,
		name:      "alloc-net1-prefix1",
		spec: ipamv1alpha1.IPAllocationSpec{
			NetworkInstance: niName,
			PrefixKind:      ipamv1alpha1.PrefixKindNetwork,
			Prefix:          "10.0.0.1/24",
			CreatePrefix:    true,
			Labels: map[string]string{
				"nephio.org/gateway":      "true",
				"nephio.org/region":       "us-central1",
				"nephio.org/site":         "edge1",
				"nephio.org/network-name": "net1",
			},
		},
	}

	// create new rib
	ipam := New(nil)
	// create new networkinstance
	niCr := buildNetworkInstance(niCreate)
	if err := ipam.Create(context.Background(), niCr); err != nil {
		t.Errorf("%v occured, cannot create network instance: %s/%s", err, niCr.GetNamespace(), niCr.GetName())
	}
	allocNiPrefix := buildIPAllocation(niPrefixAlloc)
	allocNiPrefixResp, err := ipam.AllocateIPPrefix(context.Background(), allocNiPrefix)
	if err != nil {
		t.Errorf("%v, cannot create ip prefix: %v", err, allocNiPrefixResp)
		return
	}
	if allocNiPrefixResp.Status.AllocatedPrefix != niPrefixAlloc.spec.Prefix {
		t.Errorf("expected prefix %s, got %s", niPrefixAlloc.spec.Prefix, allocNiPrefixResp.Status.AllocatedPrefix)
	}

	// create prefix w/o parent network prefix -> should fail
	allocNet1PrefixWoCreate := buildIPAllocation(net1PrefixAllocWoCreate)
	allocNet1PrefixWoCreateResp, err := ipam.AllocateIPPrefix(context.Background(), allocNet1PrefixWoCreate)
	if err == nil {
		t.Errorf("expecting error: %s, got %v resp: %v", errValidateNetworkPrefixWoNetworkParent, err, allocNet1PrefixWoCreateResp)
		return
	} else {
		if !strings.Contains(err.Error(), errValidateNetworkPrefixWoNetworkParent) {
			t.Errorf("expecting error: %s, got %v resp: %v", errValidateNetworkPrefixWoNetworkParent, err, allocNet1PrefixWoCreateResp)
			return
		}
	}

	// create parent network prefix
	allocNet1PrefixCreate := buildIPAllocation(net1PrefixAllocCreate)
	allocNet1PrefixCreateResp, err := ipam.AllocateIPPrefix(context.Background(), allocNet1PrefixCreate)
	if err != nil {
		t.Errorf("%v, cannot create ip prefix: %v", err, allocNet1PrefixCreateResp)
		return
	}
	if allocNet1PrefixCreateResp.Status.AllocatedPrefix != net1PrefixAllocCreate.spec.Prefix {
		t.Errorf("expected prefix %s, got %s", net1PrefixAllocCreate.spec.Prefix, allocNet1PrefixCreateResp.Status.AllocatedPrefix)
	}

	// create parent network prefix
	allocNet1PrefixWoCreateResp, err = ipam.AllocateIPPrefix(context.Background(), allocNet1PrefixWoCreate)
	if err != nil {
		t.Errorf("%v, cannot create ip prefix: %v", err, allocNet1PrefixCreateResp)
		return
	}
	if allocNet1PrefixWoCreateResp.Status.AllocatedPrefix != net1PrefixAllocWoCreate.spec.Prefix {
		t.Errorf("expected prefix %s, got %s", net1PrefixAllocWoCreate.spec.Prefix, allocNet1PrefixWoCreateResp.Status.AllocatedPrefix)
	}
	routes := ipam.GetPrefixes(niCr)
	for _, route := range routes {
		fmt.Println(route)
	}

}
