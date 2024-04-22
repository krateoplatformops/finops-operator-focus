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

	finopsv1 "github.com/krateoplatformops/finops-operator-focus/api/v1"
	"github.com/krateoplatformops/finops-operator-focus/internal/utils"

	appsv1 "k8s.io/api/apps/v1"
)

type DeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=finops.krateo.io,resources=focusconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.krateo.io,resources=focusconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=finops.krateo.io,resources=focusconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=finops.krateo.io,resources=scraperconfigs,verbs=get;create;update
//+kubebuilder:rbac:groups=finops.krateo.io,resources=databaseconfigs,verbs=get;create;update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;create;list;update

func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("FinOps.V1", "Deployment")
	var err error

	var deployment appsv1.Deployment
	// Deployment does not exist, check if the focusConfig exists
	if err = r.Get(ctx, req.NamespacedName, &deployment); err != nil {
		logger.Info("unable to fetch appsv1.Deployment " + req.Name + " " + req.Namespace)
		focusConfig, err := r.getConfigurationCR(ctx, strings.Replace(req.Name, "-deployment", "", 1), req.Namespace)
		if err != nil {
			logger.Info("Unable to fetch focusConfig for " + strings.Replace(req.Name, "-deployment", "", 1) + " " + req.Namespace)
			return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
		}
		err = r.createRestoreDeploymentAgain(ctx, focusConfig, false)
		if err != nil {
			logger.Error(err, "Unable to create Deployment again "+req.Name+" "+req.Namespace)
			return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
		}
		logger.Info("Created deployment again: " + req.Name + " " + req.Namespace)

	}

	if ownerReferences := deployment.GetOwnerReferences(); len(ownerReferences) > 0 {
		if ownerReferences[0].Kind == "focusConfig" {
			logger.Info("Called for " + req.Name + " " + req.Namespace + " owner: " + ownerReferences[0].Kind)
			focusConfig, err := r.getConfigurationCR(ctx, strings.Replace(req.Name, "-deployment", "", 1), req.Namespace)
			if err != nil {
				return ctrl.Result{}, err
			}
			if !checkDeployment(deployment, focusConfig) {
				err = r.createRestoreDeploymentAgain(ctx, focusConfig, true)
				if err != nil {
					return ctrl.Result{}, err
				}
				logger.Info("Updated deployment: " + req.Name + " " + req.Namespace)
			}
		}
	} else {
		return ctrl.Result{Requeue: false}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Complete(r)
}

func (r *DeploymentReconciler) getConfigurationCR(ctx context.Context, name string, namespace string) (finopsv1.FocusConfig, error) {
	var focusConfig finopsv1.FocusConfig
	configurationName := types.NamespacedName{Name: name, Namespace: namespace}
	if err := r.Get(ctx, configurationName, &focusConfig); err != nil {
		log.Log.Error(err, "unable to fetch finopsv1.focusConfig")
		return finopsv1.FocusConfig{}, err
	}
	return focusConfig, nil
}

func (r *DeploymentReconciler) createRestoreDeploymentAgain(ctx context.Context, focusConfig finopsv1.FocusConfig, restore bool) error {
	genericExporterDeployment, err := utils.GetGenericExporterDeployment(focusConfig)
	if err != nil {
		return err
	}
	if restore {
		err = r.Update(ctx, genericExporterDeployment)
	} else {
		err = r.Create(ctx, genericExporterDeployment)
	}
	if err != nil {
		return err
	}
	return nil
}

func checkDeployment(deployment appsv1.Deployment, focusConfig finopsv1.FocusConfig) bool {
	if deployment.Name != focusConfig.Name+"-deployment" {
		log.Log.Info("Name does not respect naming convention")
		return false
	}

	ownerReferencesLive := deployment.OwnerReferences
	if len(ownerReferencesLive) != 1 {
		log.Log.Info("Owner reference length not one")
		return false
	}

	if ownerReferencesLive[0].Kind != focusConfig.Kind ||
		ownerReferencesLive[0].Name != focusConfig.Name ||
		ownerReferencesLive[0].UID != focusConfig.UID ||
		ownerReferencesLive[0].APIVersion != focusConfig.APIVersion {
		log.Log.Info("Owner reference wrong")
		return false
	}

	if *deployment.Spec.Replicas != 1 {
		log.Log.Info("Replicas not one", "replicas", deployment.Spec.Replicas)
		return false
	}

	if len(deployment.Spec.Selector.MatchLabels) == 0 {
		log.Log.Info("Selector not found")
		return false
	} else if deployment.Spec.Selector.MatchLabels["scraper"] != focusConfig.Name {
		log.Log.Info("Selector label scraper not equal to config name")
		return false
	}

	if len(deployment.Spec.Template.ObjectMeta.Labels) == 0 {
		log.Log.Info("No labels found")
		return false
	} else if deployment.Spec.Template.ObjectMeta.Labels["scraper"] != focusConfig.Name {
		log.Log.Info("Label scraper not equal to config name")
		return false
	}

	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		log.Log.Info("Container not equal to 1")
		return false
	} else {
		if len(deployment.Spec.Template.Spec.Containers[0].VolumeMounts) == 0 {
			log.Log.Info("No volume mount found")
			return false
		} else {
			found := false
			for _, volumeMount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
				if volumeMount.Name == "config-volume" && volumeMount.MountPath == "/temp" {
					found = true
				}
			}
			if !found {
				log.Log.Info("Volume mount not found")
				return false
			}
		}

		if len(deployment.Spec.Template.Spec.Volumes) == 0 {
			log.Log.Info("No volumes found")
			return false
		} else {
			found := false
			for _, volume := range deployment.Spec.Template.Spec.Volumes {
				if volume.Name == "config-volume" && volume.VolumeSource.ConfigMap.LocalObjectReference.Name == focusConfig.Name+"-configmap" {
					found = true
				}
			}
			if !found {
				log.Log.Info("Volume not found")
				return false
			}
		}
	}

	// Container image and secret name are not checked on purpose, since they may need to be different from the default values

	return true
}
