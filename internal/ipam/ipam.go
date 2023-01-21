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

package ipam

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/hansthienpondt/nipam/pkg/table"
	ipamv1alpha1 "github.com/nokia/k8s-ipam/apis/ipam/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Option can be used to manipulate Options.
type Option func(Ipam)

type Ipam interface {
	// Create and initialize the IPAM instance
	Create(ctx context.Context, cr *ipamv1alpha1.NetworkInstance) error
	// Delete the ipam instance
	Delete(ctx context.Context, cr *ipamv1alpha1.NetworkInstance)
	// Add a dynamic watch with callback to the ipam rib
	AddWatch(ownerGvkKey, ownerGvk string, fn CallbackFn)
	// Delete a dynamic watch with callback to the ipam rib
	DeleteWatch(ownerGvkKey, ownerGvk string)
	// AllocateIPPrefix allocates an ip prefix
	AllocateIPPrefix(ctx context.Context, cr *ipamv1alpha1.IPAllocation) (*ipamv1alpha1.IPAllocation, error)
	// DeAllocateIPPrefix
	DeAllocateIPPrefix(ctx context.Context, cr *ipamv1alpha1.IPAllocation) error
	// GetPrefixes
	GetPrefixes(cr *ipamv1alpha1.NetworkInstance) table.Routes
}

func New(c client.Client, opts ...Option) Ipam {
	ipamRib := newIpamRib()
	watcher := newWatcher()
	runtimes := NewRuntimes(&RuntimeConfig{
		ipamRib: ipamRib,
		watcher: watcher,
	})

	backend := NewNopBackend()
	if c != nil {
		backend = NewConfigMapBackend(&BackendConfig{
			client:   c,
			ipamRib:  ipamRib,
			runtimes: runtimes,
		})
	}

	i := &ipam{
		ipamRib:  ipamRib,
		runtimes: runtimes,
		backend:  backend,
		c:        c,
		watcher:  watcher,
	}

	for _, opt := range opts {
		opt(i)
	}

	return i
}

type ipam struct {
	c        client.Client
	watcher  Watcher
	ipamRib  ipamRib
	runtimes Runtimes
	backend  Backend

	l logr.Logger
}

func (r *ipam) AddWatch(ownerGvkKey, ownerGvk string, fn CallbackFn) {
	r.watcher.addWatch(ownerGvkKey, ownerGvk, fn)
}
func (r *ipam) DeleteWatch(ownerGvkKey, ownerGvk string) {
	r.watcher.deleteWatch(ownerGvkKey, ownerGvk)
}

// Initialize and create the ipam instance with the allocated prefixes
func (r *ipam) Create(ctx context.Context, cr *ipamv1alpha1.NetworkInstance) error {
	r.l = log.FromContext(ctx).WithValues("name", cr.GetName())

	r.l.Info("ipam create instance start", "isInitialized", r.ipamRib.isInitialized(cr.GetName()))
	// if the IPAM is not initialaized initialaize it
	// this happens upon initialization or ipam restart
	r.ipamRib.create(cr.GetName())
	if !r.ipamRib.isInitialized(cr.GetName()) {
		if err := r.backend.Restore(ctx, cr); err != nil {
			r.l.Error(err, "backend restore error")
		}

		r.l.Info("ipam create instance finished")
		return r.ipamRib.setInitialized(cr.GetName())
	}
	r.l.Info("ipam create instance already initialized")
	return nil
}

// Delete the ipam instance
func (r *ipam) Delete(ctx context.Context, cr *ipamv1alpha1.NetworkInstance) {
	r.l = log.FromContext(ctx).WithValues("name", cr.GetName())
	r.l.Info("ipam delete instance start")
	r.ipamRib.delete(cr.GetName())

	// delete the configmap
	if err := r.backend.Delete(ctx, cr); err != nil {
		r.l.Error(err, "backend delete error")
	}

	r.l.Info("ipam delete instance finished")

}

// AllocateIPPrefix allocates the prefix
func (r *ipam) AllocateIPPrefix(ctx context.Context, alloc *ipamv1alpha1.IPAllocation) (*ipamv1alpha1.IPAllocation, error) {
	r.l = log.FromContext(ctx).WithValues("name", alloc.GetName())
	r.l.Info("allocate prefix", "prefix", alloc.GetPrefix())

	// get the runtime based the following parameters
	// prefixkind
	// hasprefix -> if prefix parsing is nok we return an error
	// networkinstance -> if not initialized we get an error
	// initialized with alloc, rib and prefix if present
	op, err := r.runtimes.Get(alloc, false)
	if err != nil {
		return nil, err
	}
	msg, err := op.Validate(ctx)
	if err != nil {
		r.l.Error(err, "validation failed")
		return nil, err
	}
	if msg != "" {
		r.l.Error(fmt.Errorf("%s", msg), "validation failed")
		return nil, fmt.Errorf("validated failed: %s", msg)
	}
	updatedAlloc, err := op.Apply(ctx)
	if err != nil {
		return nil, err
	}
	r.l.Info("allocate prefix done", "updatedAlloc", updatedAlloc)
	//return updatedAlloc, r.updateConfigMap(ctx, alloc)
	return updatedAlloc, r.backend.Store(ctx, alloc)
}

func (r *ipam) DeAllocateIPPrefix(ctx context.Context, alloc *ipamv1alpha1.IPAllocation) error {
	r.l = log.FromContext(ctx)

	// get the runtime based the following parameters
	// prefixkind
	// hasprefix -> if prefix parsing is nok we return an error
	// networkinstance -> if not initialized we get an error
	// initialized with alloc, rib and prefix if present
	op, err := r.runtimes.Get(alloc, false)
	if err != nil {
		r.l.Error(err, "cannot get ipam operation map")
		return err
	}
	// we trust the create prefix since it was already allocated
	if err := op.Delete(ctx); err != nil {
		r.l.Error(err, "cannot deallocate prefix")
		return err
	}
	return r.backend.Store(ctx, alloc)
}

func (r *ipam) GetPrefixes(cr *ipamv1alpha1.NetworkInstance) table.Routes {
	rib, err := r.ipamRib.getRIB(cr.GetName(), false)
	if err != nil {
		r.l.Error(err, "cannpt get rib")
		return []table.Route{}
	}
	return rib.GetTable()
}
