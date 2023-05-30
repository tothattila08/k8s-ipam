/*
 Copyright 2023 The Nephio Authors.

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

package specializerreconciler

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/GoogleContainerTools/kpt-functions-sdk/go/fn"
	porchv1alpha1 "github.com/GoogleContainerTools/kpt/porch/api/porch/v1alpha1"
	"github.com/go-logr/logr"
	kptfilelibv1 "github.com/nephio-project/nephio/krm-functions/lib/kptfile/v1"
	"github.com/nokia/k8s-ipam/pkg/resource"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/kio/kioutil"
)

// reconciler reconciles a NetworkInstance object
type Reconciler struct {
	client.Client
	For         corev1.ObjectReference
	PorchClient client.Client
	Krmfn       fn.ResourceListProcessor

	l logr.Logger
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.l = log.FromContext(ctx).WithValues("req", req)
	r.l.Info("reconcile specializer")

	pr := &porchv1alpha1.PackageRevision{}
	if err := r.Get(ctx, req.NamespacedName, pr); err != nil {
		// There's no need to requeue if we no longer exist. Otherwise we'll be
		// requeued implicitly because we return an error.
		if resource.IgnoreNotFound(err) != nil {
			r.l.Error(err, "cannot get resource")
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), "cannot get resource")
		}
		return ctrl.Result{}, nil
	}
	// we just check for forResource conditions and we dont care if it is satisfied already
	// this allows us to refresh the allocation.
	ct := kptfilelibv1.GetConditionType(&r.For)
	if hasSpecificTypeConditions(pr.Status.Conditions, ct) {
		// get package revision resourceList
		prr := &porchv1alpha1.PackageRevisionResources{}
		if err := r.PorchClient.Get(ctx, req.NamespacedName, prr); err != nil {
			r.l.Error(err, "cannot get package revision resources")
			return ctrl.Result{}, errors.Wrap(err, "cannot get package revision resources")
		}
		// get resourceList from resources
		rl, err := GetResourceList(prr.Spec.Resources)
		if err != nil {
			r.l.Error(err, "cannot get resourceList")
			return ctrl.Result{}, errors.Wrap(err, "cannot get resourceList")
		}

		// run the function SDK
		_, err = r.Krmfn.Process(rl)
		if err != nil {
			r.l.Error(err, "function run failed")
			// TBD if we need to return here + check if kptfile is set
			//return ctrl.Result{}, errors.Wrap(err, "function run failed")
		}
		for _, o := range rl.Items {
			r.l.Info("resourceList", "data", o.String())
			// TBD what if we create new resources
			// update the resources with the latest info
			prr.Spec.Resources[o.GetAnnotation(kioutil.PathAnnotation)] = o.String()
		}
		kptfile := rl.Items.GetRootKptfile()
		if kptfile == nil {
			r.l.Error(fmt.Errorf("mandatory Kptfile is missing from the package"), "")
			return ctrl.Result{}, nil
		}

		kptf, err := kptfilelibv1.New(rl.Items.GetRootKptfile().String())
		if err != nil {
			r.l.Error(err, "cannot unmarshal kptfile")
			return ctrl.Result{}, nil
		}
		pr.Status.Conditions = getPorchCondiitons(kptf.GetConditions())
		if err = r.PorchClient.Update(ctx, prr); err != nil {
			return ctrl.Result{}, err
		}

	}
	return ctrl.Result{}, nil
}

func includeFile(path string, match []string) bool {
	for _, m := range match {
		file := filepath.Base(path)
		if matched, err := filepath.Match(m, file); err == nil && matched {
			return true
		}
	}
	return false
}

func GetResourceList(resources map[string]string) (*fn.ResourceList, error) {
	inputs := []kio.Reader{}
	for path, data := range resources {
		if includeFile(path, []string{"*.yaml", "*.yml", "Kptfile"}) {
			inputs = append(inputs, &kio.ByteReader{
				Reader: strings.NewReader(data),
				SetAnnotations: map[string]string{
					kioutil.PathAnnotation: path,
				},
				DisableUnwrapping: true,
			})
		}
	}
	var pb kio.PackageBuffer
	err := kio.Pipeline{
		Inputs:  inputs,
		Filters: []kio.Filter{},
		Outputs: []kio.Writer{&pb},
	}.Execute()
	if err != nil {
		return nil, err
	}

	rl := &fn.ResourceList{
		Items: fn.KubeObjects{},
	}
	for _, n := range pb.Nodes {
		s, err := n.String()
		if err != nil {
			return nil, err
		}
		o, err := fn.ParseKubeObject([]byte(s))
		if err != nil {
			return nil, err
		}
		if err := rl.UpsertObjectToItems(o, nil, true); err != nil {
			panic(err)
		}
	}
	return rl, nil
}