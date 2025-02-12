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
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"

	corev1 "k8s.io/api/core/v1"

	config "github.com/krateoplatformops/finops-data-types/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-focus/api/v1"
	prv1 "github.com/krateoplatformops/provider-runtime/apis/common/v1"
	"github.com/krateoplatformops/provider-runtime/pkg/controller"
	"github.com/krateoplatformops/provider-runtime/pkg/event"
	"github.com/krateoplatformops/provider-runtime/pkg/logging"
	"github.com/krateoplatformops/provider-runtime/pkg/ratelimiter"
	"github.com/krateoplatformops/provider-runtime/pkg/reconciler"
	"github.com/krateoplatformops/provider-runtime/pkg/resource"

	clientHelper "github.com/krateoplatformops/finops-operator-focus/internal/helpers/kube/client"
	"github.com/krateoplatformops/finops-operator-focus/internal/utils"
)

//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=focusconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=focusconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=focusconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=exporterscraperconfigs,verbs=get;create;update;delete
//+kubebuilder:rbac:groups=finops.krateo.io,namespace=finops,resources=databaseconfigs,verbs=get;create;update
//+kubebuilder:rbac:groups=apps,namespace=finops,resources=deployments,verbs=get;create;list;update;watch;delete
//+kubebuilder:rbac:groups=core,namespace=finops,resources=configmaps,verbs=get;create;list;update;delete
//+kubebuilder:rbac:groups=core,namespace=finops,resources=services,verbs=get;create;update;list;watch;delete

const (
	errNotFocusConfig = "managed resource is not a focus config custom resource"
)

func Setup(mgr ctrl.Manager, o controller.Options) error {
	name := reconciler.ControllerName(finopsv1.GroupKind)

	log := o.Logger.WithValues("controller", name)
	log.Info("controller", "name", name)

	recorder := mgr.GetEventRecorderFor(name)

	r := reconciler.NewReconciler(mgr,
		resource.ManagedKind(finopsv1.GroupVersionKind),
		reconciler.WithExternalConnecter(&connector{
			log:          log,
			recorder:     recorder,
			pollInterval: o.PollInterval,
		}),
		reconciler.WithPollInterval(o.PollInterval),
		reconciler.WithLogger(log),
		reconciler.WithRecorder(event.NewAPIRecorder(recorder)))

	log.Debug("polling rate", "rate", o.PollInterval)

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&finopsv1.FocusConfig{}).
		Complete(ratelimiter.New(name, r, o.GlobalRateLimiter))
}

type connector struct {
	pollInterval time.Duration
	log          logging.Logger
	recorder     record.EventRecorder
}

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (reconciler.ExternalClient, error) {
	cfg := ctrl.GetConfigOrDie()

	dynClient, err := clientHelper.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("unable to create dynamic client: %w", err)
	}

	return &external{
		cfg:             cfg,
		dynClient:       dynClient,
		sinceLastUpdate: make(map[string]time.Time),
		pollInterval:    c.pollInterval,
		log:             c.log,
		rec:             c.recorder,
	}, nil
}

type external struct {
	cfg             *rest.Config
	dynClient       *dynamic.DynamicClient
	sinceLastUpdate map[string]time.Time
	pollInterval    time.Duration
	log             logging.Logger
	rec             record.EventRecorder
}

func (c *external) Disconnect(_ context.Context) error {
	return nil // NOOP
}

func (e *external) Observe(ctx context.Context, mg resource.Managed) (reconciler.ExternalObservation, error) {
	focusConfig, ok := mg.(*finopsv1.FocusConfig)
	if !ok {
		return reconciler.ExternalObservation{}, errors.New(errNotFocusConfig)
	}

	if focusConfig.Status.GroupKey == "" {
		return reconciler.ExternalObservation{
			ResourceExists: false,
		}, nil
	}

	if time.Duration(time.Since(e.sinceLastUpdate[focusConfig.Name+focusConfig.Namespace])*time.Second) > time.Duration(300*time.Second) {
		return reconciler.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: false,
		}, nil
	} else {
		return reconciler.ExternalObservation{
			ResourceExists:   true,
			ResourceUpToDate: true,
		}, nil
	}

}

func (e *external) Create(ctx context.Context, mg resource.Managed) error {
	focusConfig, ok := mg.(*finopsv1.FocusConfig)
	if !ok {
		return errors.New(errNotFocusConfig)
	}

	focusConfig.SetConditions(prv1.Creating())

	focustConfigListUnstructured, err := clientHelper.ListObj(ctx, focusConfig.Namespace, focusConfig.APIVersion, "focusconfigs", e.dynClient)
	if err != nil {
		return fmt.Errorf("unable to list focusconfigs: %v", err)
	}

	focusConfigList := &finopsv1.FocusConfigList{}
	for _, focustConfigUnstructured := range focustConfigListUnstructured.Items {
		focusConfig := &finopsv1.FocusConfig{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(focustConfigUnstructured.Object, focusConfig)
		if err != nil {
			return fmt.Errorf("could not convert focus config unstructured list to config list: %v", err)
		}
		focusConfigList.Items = append(focusConfigList.Items, *focusConfig)
	}

	e.log.Debug("objects in focusconfiglist", "len", len(focusConfigList.Items))

	configGroupingByDatabase := utils.CreateGroupings(focusConfigList)
	for key := range configGroupingByDatabase {
		if err = utils.CreateExporterCR(ctx, focusConfig.Namespace, key); err != nil {
			return fmt.Errorf("unable to create exporters from scratch: %v", err)
		}
		for i := range configGroupingByDatabase[key] {

			unstructuredFocusConfigUptd, err := clientHelper.GetObj(ctx, &config.ObjectRef{Name: configGroupingByDatabase[key][i].Name, Namespace: configGroupingByDatabase[key][i].Namespace}, focusConfig.APIVersion, "focusconfigs", e.dynClient)
			if err != nil {
				return fmt.Errorf("could not obtain updated focus config: %v", err)
			}
			focusConfig := &finopsv1.FocusConfig{}
			_ = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredFocusConfigUptd.Object, focusConfig)
			focusConfig.Status.GroupKey = key
			unstructuredFocusConfig, err := clientHelper.ToUnstructured(focusConfig)
			if err != nil {
				return fmt.Errorf("could not obtain focus config unstructured: %v", err)
			}
			err = clientHelper.UpdateStatus(ctx, unstructuredFocusConfig, "focusconfigs", e.dynClient)
			e.log.Debug("focusconfig to update", "print", unstructuredFocusConfig)
			if err != nil {
				return fmt.Errorf("could not update focus config %s status: %v", unstructuredFocusConfig.GetName(), err)
			}
		}
	}

	e.rec.Eventf(focusConfig, corev1.EventTypeNormal, "Completed create", "object name: %s", focusConfig.Name)
	return nil
}

func (e *external) Update(ctx context.Context, mg resource.Managed) error {
	focusConfig, ok := mg.(*finopsv1.FocusConfig)
	if !ok {
		return errors.New(errNotFocusConfig)
	}

	err := e.Create(ctx, mg)
	if err != nil {
		return err
	}

	e.rec.Eventf(focusConfig, corev1.EventTypeNormal, "Completed update", "object name: %s", focusConfig.Name)
	e.sinceLastUpdate[focusConfig.Name+focusConfig.Namespace] = time.Now()
	return nil
}

func (e *external) Delete(ctx context.Context, mg resource.Managed) error {
	focusConfig, ok := mg.(*finopsv1.FocusConfig)
	if !ok {
		return errors.New(errNotFocusConfig)
	}

	groupKey := focusConfig.Status.GroupKey
	focusConfig.Status.GroupKey = ""
	unstructuredFocusConfig, err := clientHelper.ToUnstructured(focusConfig)
	if err != nil {
		return fmt.Errorf("could not obtain focus config unstructured: %v", err)
	}
	err = clientHelper.UpdateStatus(ctx, unstructuredFocusConfig, "focusconfigs", e.dynClient)
	if err != nil {
		return fmt.Errorf("could not update focus config %s status: %v", unstructuredFocusConfig.GetName(), err)
	}

	focusConfig.SetConditions(prv1.Deleting())

	focustConfigListUnstructured, err := clientHelper.ListObj(ctx, focusConfig.Namespace, focusConfig.APIVersion, "focusconfigs", e.dynClient)
	if err != nil {
		return fmt.Errorf("unable to list focusconfigs: %v", err)
	}

	focusConfigList := &finopsv1.FocusConfigList{}
	for _, focustConfigUnstructured := range focustConfigListUnstructured.Items {
		focusConfigItem := &finopsv1.FocusConfig{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(focustConfigUnstructured.Object, focusConfigItem)
		if err != nil {
			return fmt.Errorf("could not convert focus config unstructured list to config list: %v", err)
		}
		if focusConfigItem.Namespace != focusConfig.Namespace || focusConfigItem.Name != focusConfig.Name {
			focusConfigList.Items = append(focusConfigList.Items, *focusConfigItem)
		}
	}

	configGroupingByDatabase := utils.CreateGroupings(focusConfigList)
	_, ok = configGroupingByDatabase[groupKey]
	if !ok {
		var deploymentName string
		if groupKey != ">>" {
			deploymentName = utils.MakeGroupKeyKubeCompliant(strings.Split(groupKey, ">")[2]) + "-exporter"
		} else {
			deploymentName = "all-cr-exporter"
		}

		if len(deploymentName) > 44 {
			deploymentName = deploymentName[len(deploymentName)-44:]
		}
		e.log.Debug("deployment deleted", "deployment", deploymentName)
		clientSetLocal, err := utils.GetClientSet()
		if err != nil {
			panic(err)
		}
		utils.DeleteExporterScraperConfig(context.Background(), clientSetLocal, focusConfig.Namespace, deploymentName)
	}

	e.rec.Eventf(focusConfig, corev1.EventTypeNormal, "Received delete event", "removed groupkey")
	return nil
}
