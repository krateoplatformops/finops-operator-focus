package watcher

import (
	"context"
	"encoding/json"
	"strings"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	finopsv1 "github.com/krateoplatformops/finops-operator-focus/api/v1"
	utils "github.com/krateoplatformops/finops-operator-focus/internal/utils"
)

func StartWatcher(namespace string) {
	inClusterConfig, _ := rest.InClusterConfig()

	inClusterConfig.APIPath = "/apis"
	inClusterConfig.GroupVersion = &finopsv1.GroupVersion

	clientSet, err := dynamic.NewForConfig(inClusterConfig)
	if err != nil {
		panic(err)
	}

	fac := dynamicinformer.NewFilteredDynamicSharedInformerFactory(clientSet, 0, namespace, nil)
	informer := fac.ForResource(schema.GroupVersionResource{
		Group:    finopsv1.GroupVersion.Group,
		Version:  finopsv1.GroupVersion.Version,
		Resource: "focusconfigs",
	}).Informer()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			item := obj.(*unstructured.Unstructured)
			statusMap := item.Object["status"].(map[string]interface{})
			groupKey := statusMap["groupKey"].(string)

			gvr := schema.GroupVersionResource{
				Group:    finopsv1.GroupVersion.Group,
				Version:  finopsv1.GroupVersion.Version,
				Resource: "focusconfigs",
			}

			res := clientSet.Resource(gvr).Namespace(namespace)
			list, err := res.List(context.Background(), v1.ListOptions{})
			if err != nil {
				panic(err)
			}

			jsonData, err := list.MarshalJSON()
			if err != nil {
				panic(err)
			}

			var focusConfigList finopsv1.FocusConfigList
			err = json.Unmarshal(jsonData, &focusConfigList)
			if err != nil {
				panic(err)
			}

			configGroupingByDatabase := utils.CreateGroupings(focusConfigList)
			_, ok := configGroupingByDatabase[groupKey]
			if !ok {
				deploymentName := utils.MakeGroupKeyKubeCompliant(strings.Split(groupKey, ">")[2]) + "-exporter"
				if len(deploymentName) > 44 {
					deploymentName = deploymentName[len(deploymentName)-44:]
				}
				clientSetLocal, err := utils.GetClientSet()
				if err != nil {
					panic(err)
				}
				utils.DeleteExporterScraperConfig(context.Background(), clientSetLocal, namespace, deploymentName)
			}
		},
	})

	go informer.Run(make(<-chan struct{}))
}
