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
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	finopsv1 "github.com/krateoplatformops/finops-operator-focus/api/v1"
	"github.com/krateoplatformops/finops-operator-focus/internal/utils"

	corev1 "k8s.io/api/core/v1"
)

type ConfigMapReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ConfigMapReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("FinOps.V1", "CONFIGMAP")
	var err error
	groupKey := strings.Replace(strings.Replace(req.Name, "-configmap", "", 1), "focus-", "", 1)
	focusConfigList, err := r.getConfigurationCRList(ctx)
	if err != nil {
		logger.Info("Unable to fetch focusConfig for " + strings.Replace(req.Name, "-configmap", "", 1) + " " + req.Namespace)
		return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
	}

	configGroupingByDatabase := utils.CreateGroupings(focusConfigList)

	if len(configGroupingByDatabase[groupKey]) > 0 {
		focusConfigRoot, outputStr := utils.GetOutputStr(configGroupingByDatabase, groupKey)

		var configMap corev1.ConfigMap
		// ConfigMap does not exist, check if the focusConfig exists
		if err = r.Get(ctx, req.NamespacedName, &configMap); err != nil {
			logger.Info("unable to fetch corev1.ConfigMap " + req.Name + " " + req.Namespace)
			err = r.createRestoreConfigMapAgain(ctx, focusConfigRoot, outputStr, false)
			if err != nil {
				logger.Error(err, "Unable to create ConfigMap again "+req.Name+" "+req.Namespace)
				return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
			}
			logger.Info("Created configmap again: " + req.Name + " " + req.Namespace)
		} else {
			if !checkConfigMap(configMap, focusConfigRoot, outputStr) {
				err = r.createRestoreConfigMapAgain(ctx, focusConfigRoot, outputStr, true)
				if err != nil {
					return ctrl.Result{}, err
				}
				logger.Info("Updated configmap: " + req.Name + " " + req.Namespace)
			}
		}
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		Complete(r)
}

func (r *ConfigMapReconciler) getConfigurationCRList(ctx context.Context) (finopsv1.FocusConfigList, error) {
	var focusConfig finopsv1.FocusConfigList
	if err := r.List(ctx, &focusConfig); err != nil {
		log.Log.Error(err, "unable to list finopsv1.focusConfig")
		return finopsv1.FocusConfigList{}, err
	}
	return focusConfig, nil
}

func (r *ConfigMapReconciler) createRestoreConfigMapAgain(ctx context.Context, focusConfig finopsv1.FocusConfig, output string, restore bool) error {
	genericExporterConfigMap, err := utils.GetGenericWebserviceConfigMap(output, focusConfig)
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
