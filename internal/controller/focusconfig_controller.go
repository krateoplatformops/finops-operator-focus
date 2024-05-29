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
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	finopsv1 "github.com/krateoplatformops/finops-operator-focus/api/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

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

	var focusConfigList finopsv1.FocusConfigList
	if err = r.List(ctx, &focusConfigList, &client.ListOptions{Namespace: req.Namespace}); err != nil {
		logger.Error(err, "unable to list FocusConfig")
		return ctrl.Result{}, err
	}

	var focusConfigRequest finopsv1.FocusConfig
	// If the focusConfig was not found, then it was probably deleted
	// We need to manage the configMaps
	if err = r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, &focusConfigRequest); err != nil {
		logger.Info("FocusConfig not found", "FocusConfig name", req.Name, "FocusConfig namespace", req.Namespace)
		// Create the groups and check if one of them does not have a root element
		configGroupingByDatabase := utils.CreateGroupings(focusConfigList)
		configGroupingByDatabase, keyWithoutRoot := utils.AssignRootElementIfNotPresent(configGroupingByDatabase)
		// Update the all the focusConfig statuses
		for key := range configGroupingByDatabase {
			for _, config := range configGroupingByDatabase[key] {
				logger.Info("Updating focusConfig", "FocusConfig name", config.Name)
				if err = r.Status().Update(ctx, &config); err != nil {
					return ctrl.Result{}, err
				}
			}
		}

		// Re-create the root element to re-start the exporting-scraping pipeline
		if keyWithoutRoot != "" {
			focusConfigRoot, outputStr := utils.GetOutputStr(configGroupingByDatabase, keyWithoutRoot)
			// Create all the services, configmaps, deployments and, exporterScraperConfigs
			if err = r.createExporterFromScratch(ctx, req, focusConfigRoot, outputStr); err != nil {
				return ctrl.Result{}, err
			}
		} else { // Otherwise we do nothing, the group that lost a CR will eventually be updated by another CR change
			logger.Info("Could not identify modified focusConfig, ignoring...")
		}

		return ctrl.Result{}, client.IgnoreNotFound(err)

	}

	newGroupKey := strings.ReplaceAll(focusConfigRequest.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace+
		"-"+focusConfigRequest.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name+
		"-"+focusConfigRequest.Spec.ScraperConfig.TableName, "_", "-")
	// If the previous groupKey and the new one are the same, its grouping has not changed, we just need to update the configmap data (done by the informer)
	if focusConfigRequest.Status.GroupKey == newGroupKey {
		logger.Info("GroupKey equal to previous")
		var configMap corev1.ConfigMap
		if err = r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: "focus-" + focusConfigRequest.Status.GroupKey + "-configmap"}, &configMap); err != nil {
			return ctrl.Result{}, err
		}
		if err = r.Delete(ctx, &configMap); err != nil {
			return ctrl.Result{}, err
		} else {
			return ctrl.Result{}, nil
		}
	} else if focusConfigRequest.Status.GroupKey != newGroupKey {
		logger.Info("GroupKey different from previous")
		// If the key has changed (or it is a new CR), we need to create the groupings.
		// All FocusSpec are grouped by the database configuration reference namespace and name, as well as the upload table
		// For each group, a new set of exporter-scraper duo is generated, since they need to export to different locations if they have different
		// database configuration.
		configGroupingByDatabase := utils.CreateGroupings(focusConfigList)

		// If the groupKey already exists, the focusConfig is a new object and the focusConfig is not a root element
		// just delete the configMap and update the status, the configmap will be recreated by the informer
		if len(configGroupingByDatabase[newGroupKey]) > 1 && focusConfigRequest.Status.GroupKey == "" && focusConfigRequest.Name != focusConfigRequest.Status.FocusConfigRoot {
			logger.Info("GroupKey already exists, delete configMap")
			focusConfigRequest.Status.GroupKey = newGroupKey
			for _, config := range configGroupingByDatabase[newGroupKey] {
				if config.Name == config.Status.FocusConfigRoot {
					focusConfigRequest.Status.FocusConfigRoot = config.Name
				}
			}
			err = r.Status().Update(ctx, &focusConfigRequest)
			if err != nil {
				return ctrl.Result{}, err
			}

			var configMap corev1.ConfigMap
			keys := []string{}
			keys = append(append(keys, focusConfigRequest.Status.GroupKey), newGroupKey)
			for _, key := range keys {
				if err = r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: "focus-" + key + "-configmap"}, &configMap); err != nil {
					return ctrl.Result{}, err
				}
				if err = r.Delete(ctx, &configMap); err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{}, nil

		}

		// If the focusConfig from the request is the root element (the one that is connected to the exporter config)
		// we need to delete all the related objects to update them since at least two groups have been modified
		if focusConfigRequest.Status.FocusConfigRoot == focusConfigRequest.Name {
			logger.Info("Request object was root")
			if err = r.deleteAllObjects(ctx, focusConfigRequest); err != nil {
				logger.Error(err, "There were errors while deleting objects. Continuing...")
			}
			// We assign a new root to the previous group (the modified object has been moved and it was the root)
			if len(configGroupingByDatabase[focusConfigRequest.Status.GroupKey]) > 0 {
				logger.Info("New root assigned to previous groupKey", "previous key", focusConfigRequest.Status.GroupKey, "new root", configGroupingByDatabase[focusConfigRequest.Status.GroupKey][0].Name)
				for i := range configGroupingByDatabase[focusConfigRequest.Status.GroupKey] {
					configGroupingByDatabase[focusConfigRequest.Status.GroupKey][i].Status.FocusConfigRoot = configGroupingByDatabase[focusConfigRequest.Status.GroupKey][0].Name
				}
			}
		}

		// We find the new group where the modified focusConfig goes
		// Then we find the root object and assign it
		for key := range configGroupingByDatabase {
			deleteInThisBlock := false
			for i, config := range configGroupingByDatabase[key] {
				if config.Status.GroupKey != key {
					deleteInThisBlock = true
				}
				if config.Status.FocusConfigRoot == config.Name && deleteInThisBlock {
					if err = r.deleteAllObjects(ctx, config); err != nil {
						logger.Error(err, "There were errors while deleting objects. Continuing...")
					}
				}
				configGroupingByDatabase[key][i].Status.GroupKey = key
			}
		}

		// Add the root element in case there isn't one (needed for new groups)
		configGroupingByDatabase, _ = utils.AssignRootElementIfNotPresent(configGroupingByDatabase)

		// Update the all the focusConfig statuses
		for key := range configGroupingByDatabase {
			for _, config := range configGroupingByDatabase[key] {
				logger.Info("Updating focusConfig", "FocusConfig name", config.Name)
				if err = r.Status().Update(ctx, &config); err != nil {
					return ctrl.Result{}, err
				}
			}
		}

		// Compose the CSV file from each CR in each group
		for key := range configGroupingByDatabase {
			logger.Info("GroupKeys present in grouping", "key", key)
			focusConfigRoot, outputStr := utils.GetOutputStr(configGroupingByDatabase, key)
			// Create all the services, configmaps, deployments and, exporterScraperConfigs
			if err = r.createExporterFromScratch(ctx, req, focusConfigRoot, outputStr); err != nil {
				return ctrl.Result{}, err
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

func (r *FocusConfigReconciler) createExporterFromScratch(ctx context.Context, req ctrl.Request, focusConfig finopsv1.FocusConfig, output string) error {
	log.Log.Info("createExporterFromScratch Start")
	var err error
	// Create the ConfigMap object first
	// Check if the ConfigMap exists in the cluster
	genericWebserviceConfigMap := &corev1.ConfigMap{}
	_ = r.Get(context.Background(), types.NamespacedName{
		Namespace: req.Namespace,
		Name:      "focus-" + focusConfig.Status.GroupKey + "-configmap",
	}, genericWebserviceConfigMap)
	// If it does not exist, create it in the cluster
	if genericWebserviceConfigMap.ObjectMeta.Name == "" {
		genericWebserviceConfigMap, err = utils.GetGenericWebserviceConfigMap(output, focusConfig)
		if err != nil {
			return err
		}
		err = r.Create(ctx, genericWebserviceConfigMap)
		if err != nil {
			return err
		}
	}

	// Create the generic webservice deployment
	genericWebserviceDeployment := &appsv1.Deployment{}
	_ = r.Get(context.Background(), types.NamespacedName{
		Namespace: req.Namespace,
		Name:      focusConfig.Name + "-webservice-generic-deployment",
	}, genericWebserviceDeployment)
	if genericWebserviceDeployment.ObjectMeta.Name == "" {
		genericWebserviceDeployment, err = utils.GetGenericWebserviceDeployment(focusConfig)
		if err != nil {
			return err
		}
		// Create the actual deployment
		err = r.Create(ctx, genericWebserviceDeployment)
		if err != nil {
			return err
		}
	}

	// Create the Service
	// Check if the Service exists
	genericWebserviceService := &corev1.Service{}
	_ = r.Get(context.Background(), types.NamespacedName{
		Namespace: req.Namespace,
		Name:      focusConfig.Name + "-service",
	}, genericWebserviceService)
	// If it does not exist, create it
	if genericWebserviceService.ObjectMeta.Name == "" {
		genericWebserviceService, _ = utils.GetGenericWebserviceService(focusConfig)
		err = r.Create(ctx, genericWebserviceService)
		if err != nil {
			return err
		}
	}

	serviceIp := genericWebserviceService.Spec.ClusterIP
	servicePort := -1
	for _, port := range genericWebserviceService.Spec.Ports {
		servicePort = int(port.TargetPort.IntVal)
	}

	// Create the CR to start the Exporter Operator
	err = utils.CreateExporterCR(ctx, focusConfig, serviceIp, servicePort)
	if err != nil {
		return err
	}
	log.Log.Info("createExporterFromScratch Finish")
	return nil
}

func (r *FocusConfigReconciler) deleteAllObjects(ctx context.Context, focusConfig finopsv1.FocusConfig) error {
	genericWebserviceConfigMap, err := utils.GetGenericWebserviceConfigMap("to delete", focusConfig)
	if err != nil {
		return err
	}
	log.Log.Info("Deleting configmap", "ConfigMap Name", genericWebserviceConfigMap.Name, "ConfigMap Namespace", genericWebserviceConfigMap.Namespace)
	err = r.Delete(ctx, genericWebserviceConfigMap)
	if err != nil {
		return err
	}

	genericWebserviceDeployment, err := utils.GetGenericWebserviceDeployment(focusConfig)
	if err != nil {
		return err
	}
	log.Log.Info("Deleting deployment", "Deployment name", genericWebserviceDeployment.Name)
	err = r.Delete(ctx, genericWebserviceDeployment)
	if err != nil {
		return err
	}

	genericWebserviceService, _ := utils.GetGenericWebserviceService(focusConfig)
	log.Log.Info("Deleting service", "Service name", genericWebserviceService.Name)
	err = r.Delete(ctx, genericWebserviceService)
	if err != nil {
		return err
	}

	clientset, err := utils.GetClientSet()
	log.Log.Info("Deleting ExporterScraperConfig")
	if err != nil {
		return err
	}
	utils.DeleteExporterScraperConfig(ctx, focusConfig, clientset)

	return nil
}
