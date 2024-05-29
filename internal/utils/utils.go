package utils

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	operatorPackage "github.com/krateoplatformops/finops-operator-exporter/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-focus/api/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func CreateExporterCR(ctx context.Context, namespace string, groupKey string) error {
	clientset, err := GetClientSet()
	if err != nil {
		return err
	}
	DeleteExporterScraperConfig(ctx, clientset, namespace, groupKey)
	for CheckExporterScraperConfigDeletion(ctx, clientset, namespace, groupKey) {
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	url := "http://<kubernetes_host>:<kubernetes_port>" + "/apis/finops.krateo.io/v1/namespaces/finops/focusconfigs?fieldSelector=status.groupKey%3D" + groupKey + "&limit=500/"
	exporterScraperConfig := GetExporterScraperObject(namespace, groupKey, url)
	jsonData, err := json.Marshal(exporterScraperConfig)
	if err != nil {
		return err
	}
	_, err = clientset.RESTClient().Post().
		AbsPath("/apis/finops.krateo.io/v1").
		Namespace(namespace).
		Resource("exporterscraperconfigs").
		Name(makeGroupKeyKubeCompliant(groupKey) + "-exporter").
		Body(jsonData).
		DoRaw(ctx)
	if err != nil {
		return err
	}

	return nil
}

func GetExporterScraperObject(namespace string, groupKey string, url string) *operatorPackage.ExporterScraperConfig {
	groupKeyKubeCompliant := makeGroupKeyKubeCompliant(groupKey)
	additionalVariables := make(map[string]string)
	additionalVariables["certFilePath"] = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	return &operatorPackage.ExporterScraperConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ExporterScraperConfig",
			APIVersion: "finops.krateo.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      groupKeyKubeCompliant + "-exporter",
			Namespace: namespace,
			/*
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: focusConfig.APIVersion,
						Kind:       focusConfig.Kind,
						Name:       focusConfig.Name,
						UID:        focusConfig.UID,
					},
				},*/
		},
		Spec: operatorPackage.ExporterScraperConfigSpec{
			ExporterConfig: operatorPackage.ExporterConfig{
				Name:                  "CC",
				Url:                   url,
				RequireAuthentication: true,
				AuthenticationMethod:  "cert-file",
				PollingIntervalHours:  6,
				AdditionalVariables:   additionalVariables,
			},
			ScraperConfig: operatorPackage.ScraperConfig{
				TableName:            strings.Split(groupKey, "=")[2],
				PollingIntervalHours: 6,
				ScraperDatabaseConfigRef: operatorPackage.ScraperDatabaseConfigRef{
					Name:      strings.Split(groupKey, "=")[1],
					Namespace: strings.Split(groupKey, "=")[0],
				},
			},
		},
	}
}

func GetClientSet() (*kubernetes.Clientset, error) {
	inClusterConfig, err := rest.InClusterConfig()
	if err != nil {
		return &kubernetes.Clientset{}, err
	}

	clientset, err := kubernetes.NewForConfig(inClusterConfig)
	if err != nil {
		return &kubernetes.Clientset{}, err
	}
	return clientset, nil
}

func DeleteExporterScraperConfig(ctx context.Context, clientset *kubernetes.Clientset, namespace string, groupKey string) {
	_, _ = clientset.RESTClient().Delete().
		AbsPath("/apis/finops.krateo.io/v1").
		Namespace(namespace).
		Resource("exporterscraperconfigs").
		Name(makeGroupKeyKubeCompliant(groupKey) + "-exporter").
		DoRaw(ctx)
}

func CheckExporterScraperConfigDeletion(ctx context.Context, clientset *kubernetes.Clientset, namespace string, groupKey string) bool {
	_, err := clientset.RESTClient().Get().
		AbsPath("/apis/finops.krateo.io/v1").
		Namespace(namespace).
		Resource("exporterscraperconfigs").
		Name(makeGroupKeyKubeCompliant(groupKey) + "-exporter").
		DoRaw(ctx)
	return err == nil
}

func CreateGroupings(focusConfigList finopsv1.FocusConfigList) map[string][]finopsv1.FocusConfig {
	configGroupingByDatabase := make(map[string][]finopsv1.FocusConfig)
	for _, config := range focusConfigList.Items {
		newGroupKey := config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace +
			"=" + config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name +
			"=" + config.Spec.ScraperConfig.TableName
		arrSoFar := configGroupingByDatabase[newGroupKey]
		configGroupingByDatabase[newGroupKey] = append(arrSoFar, config)
	}
	return configGroupingByDatabase
}

func makeGroupKeyKubeCompliant(groupKey string) string {
	return strings.ReplaceAll(strings.ReplaceAll(groupKey, "_", "-"), "=", "-")
}
