/*
Copyright 2024.

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

package controller

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	finopsv1 "github.com/krateoplatformops/finops-operator-focus/api/v1"

	utils "github.com/krateoplatformops/finops-operator-focus/internal/utils"
)

// FocusConfigReconciler reconciles a FocusConfig object
type FocusConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=focusconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=focusconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=focusconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=exporterscraperconfigs,verbs=get;create;update;delete
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=databaseconfigs,verbs=get;create;update
//+kubebuilder:rbac:groups=apps,namespace=finops,resources=deployments,verbs=get;create;list;update;watch;delete
//+kubebuilder:rbac:groups=core,namespace=finops,resources=configmaps,verbs=get;create;list;update;delete
//+kubebuilder:rbac:groups=core,namespace=finops,resources=services,verbs=get;create;update;list;watch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *FocusConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("FinOps.V1", req.NamespacedName)
	var err error

	var focusConfigReq finopsv1.FocusConfig
	if err = r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &focusConfigReq); err != nil {
		logger.Info("Could not retrieve FocusConfig, probably deleted. Informer will handle it...")
		return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
	}

	var focusConfigList finopsv1.FocusConfigList
	if err = r.List(ctx, &focusConfigList, &client.ListOptions{Namespace: req.Namespace}); err != nil {
		logger.Error(err, "unable to list FocusConfig")
		return ctrl.Result{}, err
	}

	configGroupingByDatabase := utils.CreateGroupings(focusConfigList)
	for key := range configGroupingByDatabase {
		logger.Info("Checking if there are exporters to create...")
		if err = r.createExporterFromScratch(ctx, req.Namespace, key); err != nil {
			return ctrl.Result{}, err
		}
		for i := range configGroupingByDatabase[key] {
			logger.Info("Updating status groupKey for focusConfig CRs")
			configGroupingByDatabase[key][i].Status.GroupKey = key
			err = r.Status().Update(ctx, &configGroupingByDatabase[key][i])
			if err != nil {
				return ctrl.Result{}, nil
			}
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FocusConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1.FocusConfig{}).
		Complete(r)
}

func (r *FocusConfigReconciler) createExporterFromScratch(ctx context.Context, namespace string, groupKey string) error {
	// Create the CR to start the Exporter Operator
	err := utils.CreateExporterCR(ctx, namespace, groupKey)
	if err != nil {
		return err
	}
	return nil
}
