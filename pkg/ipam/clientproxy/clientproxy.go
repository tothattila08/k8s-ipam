package clientproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/henderiw-k8s-lcnc/discovery/registrator"
	allocv1alpha1 "github.com/nokia/k8s-ipam/apis/alloc/common/v1alpha1"
	ipamv1alpha1 "github.com/nokia/k8s-ipam/apis/alloc/ipam/v1alpha1"
	"github.com/nokia/k8s-ipam/internal/meta"
	"github.com/nokia/k8s-ipam/pkg/alloc/allocpb"
	"github.com/nokia/k8s-ipam/pkg/proxycache"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type Proxy interface {
	AddEventChs(map[schema.GroupVersionKind]chan event.GenericEvent)
	//GetProxyCache() proxycache.ProxyCache
	// Create creates the network instance in the ipam
	Create(ctx context.Context, cr *ipamv1alpha1.NetworkInstance) error
	// Delete deletes the network instance in the ipam
	Delete(ctx context.Context, cr *ipamv1alpha1.NetworkInstance) error
	// Get returns the allocated prefix
	Get(ctx context.Context, cr client.Object, d any) (*ipamv1alpha1.IPAllocation, error)
	// AllocateIPPrefix allocates an ip prefix
	AllocateIPPrefix(ctx context.Context, cr client.Object, d any) (*ipamv1alpha1.IPAllocation, error)
	// DeAllocateIPPrefix
	DeAllocateIPPrefix(ctx context.Context, cr client.Object, d any) error
}

type Config struct {
	Registrator registrator.Registrator
}

func New(ctx context.Context, c *Config) Proxy {
	l := ctrl.Log.WithName("ipam-client-proxy")

	pc := proxycache.New(&proxycache.Config{
		Registrator: c.Registrator,
	})
	cp := &clientproxy{
		pc: pc,
		l:  l,
	}
	pc.RegisterRefreshRespValidator(ipamv1alpha1.GroupVersion.Group, cp.ValidateIpamResponse)
	pc.Start(ctx)
	return cp
}

type clientproxy struct {
	pc proxycache.ProxyCache
	//logger
	l logr.Logger
}

func (r *clientproxy) GetProxyCache() proxycache.ProxyCache {
	return r.pc
}

func (r *clientproxy) AddEventChs(ec map[schema.GroupVersionKind]chan event.GenericEvent) {
	r.pc.AddEventChs(ec)
}

func (r *clientproxy) Create(ctx context.Context, cr *ipamv1alpha1.NetworkInstance) error {
	ownerGvk := meta.GetGVKFromAPIVersionKind(cr.APIVersion, cr.Kind)
	gvk := meta.GetGVKFromObject(cr)
	b, err := json.Marshal(cr)
	if err != nil {
		return err
	}
	req := buildAllocPb(cr, cr.GetName(), string(b), "never", gvk, ownerGvk)
	_, err = r.pc.Allocate(ctx, req)
	return err
}

func (r *clientproxy) Delete(ctx context.Context, cr *ipamv1alpha1.NetworkInstance) error {
	ownerGvk := meta.GetGVKFromAPIVersionKind(cr.APIVersion, cr.Kind)
	gvk := meta.GetGVKFromObject(cr)
	b, err := json.Marshal(cr)
	if err != nil {
		return err
	}
	req := buildAllocPb(cr, cr.GetName(), string(b), "never", gvk, ownerGvk)
	return r.pc.DeAllocate(ctx, req)
}

func (r *clientproxy) Get(ctx context.Context, o client.Object, d any) (*ipamv1alpha1.IPAllocation, error) {
	r.l.Info("get allocated prefix", "cr", o)
	// normalizes the input to the proxycache generalized allocation
	req, err := NormalizeKRMToProxyCacheAllocation(o, d)
	if err != nil {
		return nil, err
	}
	r.l.Info("get allocated prefix", "allobrequest", req)
	resp, err := r.pc.Get(ctx, req)
	if err != nil {
		return nil, err
	}
	ipAlloc := &ipamv1alpha1.IPAllocation{}
	if err := json.Unmarshal([]byte(resp.Status), ipAlloc); err != nil {
		return nil, err
	}
	r.l.Info("allocate prefix done", "result", ipAlloc.Status)
	return ipAlloc, nil

}

func (r *clientproxy) AllocateIPPrefix(ctx context.Context, o client.Object, d any) (*ipamv1alpha1.IPAllocation, error) {
	r.l.Info("allocate prefix", "cr", o)
	// normalizes the input to the proxycache generalized allocation
	req, err := NormalizeKRMToProxyCacheAllocation(o, d)
	if err != nil {
		return nil, err
	}
	r.l.Info("allocate prefix", "allobrequest", req)

	resp, err := r.pc.Allocate(ctx, req)
	if err != nil {
		return nil, err
	}
	ipAlloc := &ipamv1alpha1.IPAllocation{}
	if err := json.Unmarshal([]byte(resp.Status), ipAlloc); err != nil {
		return nil, err
	}
	r.l.Info("allocate prefix done", "result", ipAlloc.Status)
	return ipAlloc, nil
}

func (r *clientproxy) DeAllocateIPPrefix(ctx context.Context, o client.Object, d any) error {
	// normalizes the input to the proxycache generalized allocation
	req, err := NormalizeKRMToProxyCacheAllocation(o, d)
	if err != nil {
		return err
	}
	return r.pc.DeAllocate(ctx, req)
}

// NormalizeKRMToProxyCacheAllocation normalizes the input to a genralized allocation request
func NormalizeKRMToProxyCacheAllocation(o client.Object, d any) (*allocpb.Request, error) {
	switch o.GetObjectKind().GroupVersionKind().Kind {
	case ipamv1alpha1.IPPrefixKind:
		cr, ok := o.(*ipamv1alpha1.IPPrefix)
		if !ok {
			return nil, fmt.Errorf("unexpected error casting object to IPPrefix failed")
		}
		return BuildAllocationFromIPPrefix(cr)
	case ipamv1alpha1.IPAllocationKind:
		cr, ok := o.(*ipamv1alpha1.IPAllocation)
		if !ok {
			return nil, fmt.Errorf("unexpected error casting object to IPAllocation failed")
		}
		t := time.Now().Add(time.Minute * 60)
		b, err := t.MarshalText()
		if err != nil {
			return nil, err
		}
		return BuildAllocationFromIPAllocation(cr, string(b))
	case ipamv1alpha1.NetworkInstanceKind:
		cr, ok := o.(*ipamv1alpha1.NetworkInstance)
		if !ok {
			return nil, fmt.Errorf("unexpected error casting object to NetworkInstance failed")
		}
		ipPrefix, ok := d.(*ipamv1alpha1.Prefix)
		if !ok {
			return nil, fmt.Errorf("unexpected error casting object to Ip Prefix failed")
		}
		return BuildAllocationFromNetworkInstancePrefix(cr, ipPrefix)
	default:
		return nil, fmt.Errorf("cannot allocate prefix for unknown kind, got %s", o.GetObjectKind().GroupVersionKind().Kind)
	}
}

func BuildAllocationFromIPPrefix(cr *ipamv1alpha1.IPPrefix) (*allocpb.Request, error) {
	ownerGvk := meta.GetGVKFromAPIVersionKind(cr.APIVersion, cr.Kind)

	ipalloc := ipamv1alpha1.BuildIPAllocationFromIPPrefix(cr)
	b, err := json.Marshal(ipalloc)
	if err != nil {
		return nil, err
	}

	return buildAllocPb(cr, cr.GetName(), string(b), "never", getIPAllocGVK(), ownerGvk), nil
}

func BuildAllocationFromNetworkInstancePrefix(cr *ipamv1alpha1.NetworkInstance, prefix *ipamv1alpha1.Prefix) (*allocpb.Request, error) {
	ownerGvk := meta.GetGVKFromAPIVersionKind(cr.APIVersion, cr.Kind)
	ipalloc := ipamv1alpha1.BuildIPAllocationFromNetworkInstancePrefix(cr, prefix)
	b, err := json.Marshal(ipalloc)
	if err != nil {
		return nil, err
	}
	return buildAllocPb(cr, cr.GetNameFromNetworkInstancePrefix(prefix.Prefix), string(b), "never", getIPAllocGVK(), ownerGvk), nil
}

func BuildAllocationFromIPAllocation(cr *ipamv1alpha1.IPAllocation, expiryTime string) (*allocpb.Request, error) {

	ownerGvk := meta.GetGVKFromAPIVersionKind(cr.APIVersion, cr.Kind)
	// if the ownerGvk is in the labels we use this as ownerGVK
	ownerGVKValue, ok := cr.GetLabels()[allocv1alpha1.NephioOwnerGvkKey]
	if ok {
		ownerGvk = meta.StringToGVK(ownerGVKValue)
	}
	newCr := ipamv1alpha1.BuildIPAllocationFromIPAllocation(cr)

	ipalloc := ipamv1alpha1.BuildIPAllocation(cr, cr.GetName(), newCr.Spec, ipamv1alpha1.IPAllocationStatus{AllocatedPrefix: cr.Status.AllocatedPrefix})
	b, err := json.Marshal(ipalloc)
	if err != nil {
		return nil, err
	}
	return buildAllocPb(cr, cr.GetName(), string(b), expiryTime, getIPAllocGVK(), ownerGvk), nil
}

func getIPAllocGVK() *schema.GroupVersionKind {
	return &schema.GroupVersionKind{
		Group:   ipamv1alpha1.GroupVersion.Group,
		Version: ipamv1alpha1.GroupVersion.Version,
		Kind:    ipamv1alpha1.IPAllocationKind,
	}
}

func buildAllocPb(o client.Object, nsnName, specBody, expiryTime string, gvk, ownerGvk *schema.GroupVersionKind) *allocpb.Request {
	return &allocpb.Request{
		Header: &allocpb.Header{
			Gvk: &allocpb.GVK{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind,
			},
			Nsn: &allocpb.NSN{
				Namespace: o.GetNamespace(),
				Name:      nsnName, // this will be overwritten for niInstance prefixes
			},
			OwnerGvk: &allocpb.GVK{
				Group:   ownerGvk.Group,
				Version: ownerGvk.Version,
				Kind:    ownerGvk.Kind,
			},
			OwnerNsn: &allocpb.NSN{
				Namespace: o.GetNamespace(),
				Name:      o.GetName(),
			},
		},
		Spec:       specBody,
		ExpiryTime: expiryTime,
	}
}

func GetNameFromNetworkInstancePrefix(name, prefix string) string {
	return fmt.Sprintf("%s-%s-%s", name, "aggregate", strings.ReplaceAll(prefix, "/", "-"))
}

func (r *clientproxy) ValidateIpamResponse(origResp *allocpb.Response, newResp *allocpb.Response) bool {
	origAlloc := &ipamv1alpha1.IPAllocation{}
	if err := json.Unmarshal([]byte(origResp.Status), origAlloc); err != nil {
		return false
	}
	newAlloc := &ipamv1alpha1.IPAllocation{}
	if err := json.Unmarshal([]byte(origResp.Status), newAlloc); err != nil {
		return false
	}
	r.l.Info("validate ipam response",
		"orig allocatedPrefix", origAlloc.Status.AllocatedPrefix,
		"new allocatedPrefix", newAlloc.Status.AllocatedPrefix,
		"orig gateway", origAlloc.Status.Gateway,
		"new gateway", newAlloc.Status.Gateway,
	)
	if origAlloc.Status.AllocatedPrefix != newAlloc.Status.AllocatedPrefix ||
		origAlloc.Status.Gateway != newAlloc.Status.Gateway {
		return false
	}
	return true

}