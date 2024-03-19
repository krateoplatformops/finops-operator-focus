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
	"bytes"
	"context"
	"reflect"
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

type ConfigMapReconciler struct {
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

func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("FinOps.V1", "CONFIGMAP")
	var err error

	var configMap corev1.ConfigMap
	// ConfigMap does not exist, check if the focusConfig exists
	if err = r.Get(ctx, req.NamespacedName, &configMap); err != nil {
		logger.Info("unable to fetch corev1.ConfigMap " + req.Name + " " + req.Namespace)
		focusConfig, err := r.getConfigurationCR(ctx, strings.Replace(req.Name, "-configmap", "", 1), req.Namespace)
		if err != nil {
			logger.Info("Unable to fetch focusConfig for " + strings.Replace(req.Name, "-configmap", "", 1) + " " + req.Namespace)
			return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
		}

		outputStr, err := r.getOutputStr(ctx, focusConfig, req)
		if err != nil {
			return ctrl.Result{Requeue: false}, err
		}

		err = r.createRestoreConfigMapAgain(ctx, focusConfig, outputStr, false)
		if err != nil {
			logger.Error(err, "Unable to create ConfigMap again "+req.Name+" "+req.Namespace)
			return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
		}
		logger.Info("Created configmap again: " + req.Name + " " + req.Namespace)

	}

	if ownerReferences := configMap.GetOwnerReferences(); len(ownerReferences) > 0 {
		if ownerReferences[0].Kind == "FocusConfig" {
			logger.Info("Called for " + req.Name + " " + req.Namespace + " owner: " + ownerReferences[0].Kind)
			focusConfig, err := r.getConfigurationCR(ctx, strings.Replace(req.Name, "-configmap", "", 1), req.Namespace)
			if err != nil {
				return ctrl.Result{}, err
			}

			outputStr, err := r.getOutputStr(ctx, focusConfig, req)
			if err != nil {
				return ctrl.Result{Requeue: false}, err
			}

			if !checkConfigMap(configMap, focusConfig, outputStr) {
				err = r.createRestoreConfigMapAgain(ctx, focusConfig, outputStr, true)
				if err != nil {
					return ctrl.Result{}, err
				}
				logger.Info("Updated configmap: " + req.Name + " " + req.Namespace)
			}
		}
	} else {
		return ctrl.Result{Requeue: false}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		Complete(r)
}

func (r *ConfigMapReconciler) getConfigurationCR(ctx context.Context, name string, namespace string) (finopsv1.FocusConfig, error) {
	var focusConfig finopsv1.FocusConfig
	configurationName := types.NamespacedName{Name: name, Namespace: namespace}
	if err := r.Get(ctx, configurationName, &focusConfig); err != nil {
		log.Log.Error(err, "unable to fetch finopsv1.focusConfig")
		return finopsv1.FocusConfig{}, err
	}
	return focusConfig, nil
}

func (r *ConfigMapReconciler) createRestoreConfigMapAgain(ctx context.Context, focusConfig finopsv1.FocusConfig, output string, restore bool) error {
	genericExporterConfigMap, err := utils.GetGenericExporterConfigMap(output, focusConfig)
	if err != nil {
		return err
	}
	if restore {
		err = r.Update(ctx, genericExporterConfigMap)
	} else {
		err = r.Create(ctx, genericExporterConfigMap)
	}
	if err != nil {
		return err
	}
	return nil
}

func checkConfigMap(configMap corev1.ConfigMap, focusConfig finopsv1.FocusConfig, output string) bool {
	if configMap.Name != focusConfig.Name+"-configmap" {
		return false
	}

	currentData := []byte(output)

	binaryData := configMap.BinaryData
	if dataFromLive, ok := binaryData["config.yaml"]; ok {
		if !bytes.Equal(currentData, dataFromLive) {
			return false
		}
	} else {
		return false
	}

	ownerReferencesLive := configMap.OwnerReferences
	if len(ownerReferencesLive) != 1 {
		return false
	}

	if ownerReferencesLive[0].Kind != focusConfig.Kind ||
		ownerReferencesLive[0].Name != focusConfig.Name ||
		ownerReferencesLive[0].UID != focusConfig.UID ||
		ownerReferencesLive[0].APIVersion != focusConfig.APIVersion {
		return false
	}

	return true
}

func (r *ConfigMapReconciler) getOutputStr(ctx context.Context, focusConfig finopsv1.FocusConfig, req ctrl.Request) (string, error) {
	var err error
	var focusConfigList finopsv1.FocusConfigList
	if err = r.List(ctx, &focusConfigList, &client.ListOptions{Namespace: req.Namespace}); err != nil {
		log.Log.Error(err, "unable to list FocusConfig")
		return "", err
	}
	configGroupingByDatabase := []finopsv1.FocusConfig{}
	for _, config := range focusConfigList.Items {
		if config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace+
			"="+config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name+
			"="+config.Spec.ScraperConfig.TableName ==
			focusConfig.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace+
				"="+focusConfig.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name+
				"="+focusConfig.Spec.ScraperConfig.TableName {
			configGroupingByDatabase = append(configGroupingByDatabase, config)
		}
	}
	outputStr := ""
	for i, config := range configGroupingByDatabase {
		v := reflect.ValueOf(config.Spec.FocusSpec)

		if i == 0 {
			focusConfig = *config.DeepCopy()

			for i := 0; i < v.NumField(); i++ {
				outputStr += v.Type().Field(i).Name + ","
			}
			outputStr = strings.TrimSuffix(outputStr, ",") + "\n"
		}

		for i := 0; i < v.NumField(); i++ {
			outputStr += utils.GetStringValue(v.Field(i).Interface()) + ","
		}
		outputStr = strings.TrimSuffix(outputStr, ",") + "\n"

	}
	outputStr = strings.TrimSuffix(outputStr, "\n")

	return outputStr, nil
}
