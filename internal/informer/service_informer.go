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

package informer

import (
	"context"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	finopsv1 "operator-focus/api/v1"
	"operator-focus/internal/utils"

	corev1 "k8s.io/api/core/v1"
)

type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=finops.krateo.io,resources=focusconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.krateo.io,resources=focusconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=finops.krateo.io,resources=focusconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=finops.krateo.io,resources=scraperconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.krateo.io,resources=databaseconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("FinOps.V1", "SERVICE")
	var err error

	var service corev1.Service
	// Service does not exist, check if the focusConfig exists
	if err = r.Get(ctx, req.NamespacedName, &service); err != nil {
		logger.Info("unable to fetch corev1.Service " + req.Name + " " + req.Namespace)
		focusConfig, err := r.getConfigurationCR(ctx, strings.Replace(req.Name, "-service", "", 1), req.Namespace)
		if err != nil {
			logger.Info("Unable to fetch focusConfig for " + strings.Replace(req.Name, "-service", "", 1) + " " + req.Namespace)
			return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
		}
		err = r.createRestoreServiceAgain(ctx, focusConfig, false)
		if err != nil {
			logger.Error(err, "Unable to create Service again "+req.Name+" "+req.Namespace)
			return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
		}
		logger.Info("Created service again: " + req.Name + " " + req.Namespace)

	}

	if ownerReferences := service.GetOwnerReferences(); len(ownerReferences) > 0 {
		if ownerReferences[0].Kind == "focusConfig" {
			logger.Info("Called for " + req.Name + " " + req.Namespace + " owner: " + ownerReferences[0].Kind)
			focusConfig, err := r.getConfigurationCR(ctx, strings.Replace(req.Name, "-service", "", 1), req.Namespace)
			if err != nil {
				return ctrl.Result{}, err
			}
			if !checkService(service, focusConfig) {
				err = r.createRestoreServiceAgain(ctx, focusConfig, true)
				if err != nil {
					return ctrl.Result{}, err
				}
				logger.Info("Updated service: " + req.Name + " " + req.Namespace)
			}
		}
	} else {
		return ctrl.Result{Requeue: false}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Complete(r)
}

func (r *ServiceReconciler) getConfigurationCR(ctx context.Context, name string, namespace string) (finopsv1.FocusConfig, error) {
	var focusConfig finopsv1.FocusConfig
	configurationName := types.NamespacedName{Name: name, Namespace: namespace}
	if err := r.Get(ctx, configurationName, &focusConfig); err != nil {
		log.Log.Error(err, "unable to fetch finopsv1.focusConfig")
		return finopsv1.FocusConfig{}, err
	}
	return focusConfig, nil
}

func (r *ServiceReconciler) createRestoreServiceAgain(ctx context.Context, focusConfig finopsv1.FocusConfig, restore bool) error {
	genericExporterService, err := utils.GetGenericExporterService(focusConfig)
	if err != nil {
		return err
	}
	if restore {
		err = r.Update(ctx, genericExporterService)
	} else {
		err = r.Create(ctx, genericExporterService)
	}
	if err != nil {
		return err
	}
	return nil
}

func checkService(service corev1.Service, focusConfig finopsv1.FocusConfig) bool {
	if service.Name != focusConfig.Name+"-service" {
		return false
	}

	ownerReferencesLive := service.OwnerReferences
	if len(ownerReferencesLive) != 1 {
		return false
	}

	if service.Spec.Selector["scraper"] != focusConfig.Name {
		return false
	}

	if service.Spec.Type != corev1.ServiceTypeNodePort {
		return false
	}

	if len(service.Spec.Ports) == 0 {
		return false
	} else {
		found := false
		for _, port := range service.Spec.Ports {
			if port.Protocol == corev1.ProtocolTCP && port.Port == 2112 {
				found = true
			}
		}
		if !found {
			return false
		}
	}

	if ownerReferencesLive[0].Kind != focusConfig.Kind ||
		ownerReferencesLive[0].Name != focusConfig.Name ||
		ownerReferencesLive[0].UID != focusConfig.UID ||
		ownerReferencesLive[0].APIVersion != focusConfig.APIVersion {
		return false
	}

	return true
}
