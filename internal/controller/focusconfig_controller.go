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
	"reflect"
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

//+kubebuilder:rbac:groups=finops.krateo.io,resources=focusconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.krateo.io,resources=focusconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=finops.krateo.io,resources=focusconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=finops.krateo.io,resources=scraperconfigs,verbs=get;create;update
//+kubebuilder:rbac:groups=finops.krateo.io,resources=databaseconfigs,verbs=get;create;update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;create;update
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;create;update
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;create;update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *FocusConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("FinOps.V1", req.NamespacedName)
	var err error

	var curFocusConfig finopsv1.FocusConfig
	if err = r.Get(ctx, req.NamespacedName, &curFocusConfig); err != nil {
		logger.Info("unable to get current FocusConfig, probably deleted, ignoring...")
		return ctrl.Result{Requeue: false}, client.IgnoreNotFound(err)
	}

	var focusConfigList finopsv1.FocusConfigList
	if err = r.List(ctx, &focusConfigList, &client.ListOptions{Namespace: req.Namespace}); err != nil {
		logger.Error(err, "unable to list FocusConfig")
		return ctrl.Result{}, err
	}

	// All FocusSpec are grouped by the database configuration reference namespace and name, as well as the upload table
	// For each group, a new set of exporter-scraper duo is generated, since they need to export to different locations if they have different
	// database configuration
	configGroupingByDatabase := make(map[string][]finopsv1.FocusConfig)
	for i, config := range focusConfigList.Items {
		arrSoFar := configGroupingByDatabase[config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace+
			"="+config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name+
			"="+config.Spec.ScraperConfig.TableName]
		configGroupingByDatabase[config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace+
			"="+config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name+
			"="+config.Spec.ScraperConfig.TableName] = append(arrSoFar, config)
		focusConfigList.Items[i].Status.GroupKey = config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace +
			"=" + config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name +
			"=" + config.Spec.ScraperConfig.TableName
	}

	for key := range configGroupingByDatabase {
		outputStr := ""
		var focusConfig finopsv1.FocusConfig
		for i, config := range configGroupingByDatabase[key] {
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
		if err = r.createExporterFromScratch(ctx, req, focusConfig, outputStr); err != nil {
			return ctrl.Result{}, err
		} else if err = r.checkExporterStatus(ctx, focusConfig, outputStr); err != nil {
			return ctrl.Result{}, err
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

	var err error
	// Create the ConfigMap first
	// Check if the ConfigMap exists
	genericExporterConfigMap := &corev1.ConfigMap{}
	_ = r.Get(context.Background(), types.NamespacedName{
		Namespace: req.Namespace,
		Name:      focusConfig.Name + "-configmap",
	}, genericExporterConfigMap)
	// If it does not exist, create it
	if genericExporterConfigMap.ObjectMeta.Name == "" {
		genericExporterConfigMap, err = utils.GetGenericExporterConfigMap(output, focusConfig)
		if err != nil {
			return err
		}
		err = r.Create(ctx, genericExporterConfigMap)
		if err != nil {
			return err
		}
	}

	// Create the generic exporter deployment
	// Create the generic exporter deployment
	genericExporterDeployment := &appsv1.Deployment{}
	_ = r.Get(context.Background(), types.NamespacedName{
		Namespace: req.Namespace,
		Name:      focusConfig.Name + "-deployment",
	}, genericExporterDeployment)
	if genericExporterDeployment.ObjectMeta.Name == "" {
		genericExporterDeployment, err = utils.GetGenericExporterDeployment(focusConfig)
		if err != nil {
			return err
		}
		// Create the actual deployment
		err = r.Create(ctx, genericExporterDeployment)
		if err != nil {
			return err
		}
	}

	// Create the Service
	// Check if the Service exists
	genericExporterService := &corev1.Service{}
	_ = r.Get(context.Background(), types.NamespacedName{
		Namespace: req.Namespace,
		Name:      focusConfig.Name + "-service",
	}, genericExporterService)
	// If it does not exist, create it
	if genericExporterService.ObjectMeta.Name == "" {
		genericExporterService, _ = utils.GetGenericExporterService(focusConfig)
		err = r.Create(ctx, genericExporterService)
		if err != nil {
			return err
		}
	}

	serviceIp := genericExporterService.Spec.ClusterIP
	servicePort := -1
	for _, port := range genericExporterService.Spec.Ports {
		servicePort = int(port.TargetPort.IntVal)
	}

	// Create the CR to start the Scraper Operator

	err = utils.CreateScraperCR(ctx, focusConfig, serviceIp, servicePort)
	if err != nil {
		return err
	}
	return nil
}

func (r *FocusConfigReconciler) checkExporterStatus(ctx context.Context, focusConfig finopsv1.FocusConfig, output string) error {
	genericExporterConfigMap, err := utils.GetGenericExporterConfigMap(output, focusConfig)
	if err != nil {
		return err
	}
	genericExporterDeployment, _ := utils.GetGenericExporterDeployment(focusConfig)
	genericExporterService, _ := utils.GetGenericExporterService(focusConfig)

	err = r.Update(ctx, genericExporterConfigMap)
	if err != nil {
		return err
	}

	err = r.Update(ctx, genericExporterDeployment)
	if err != nil {
		return err
	}

	err = r.Update(ctx, genericExporterService)
	if err != nil {
		return err
	}

	return nil
}
