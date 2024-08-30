package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	finopsDataTypes "github.com/krateoplatformops/finops-data-types/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-focus/api/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func CreateExporterCR(ctx context.Context, namespace string, groupKey string) error {
	clientset, err := GetClientSet()
	if err != nil {
		return err
	}

	var deploymentName string
	if groupKey != ">>" {
		deploymentName = MakeGroupKeyKubeCompliant(strings.Split(groupKey, ">")[2]) + "-exporter"
	} else {
		deploymentName = "all-cr-exporter"
	}
	url := "https://<kubernetes_host>:<kubernetes_port>" + "/apis/finops.krateo.io/v1/namespaces/finops/focusconfigs?fieldSelector=status.groupKey=" + groupKey + "&limit=500"
	// This check is used to avoid problems with the maximum length of object names in kubernetes (63)
	// The longest appended portion is "-scraper-deployment", which is 19 characters, thus the 44
	if len(deploymentName) > 44 {
		deploymentName = deploymentName[len(deploymentName)-44:]
	}

	exporterScraperConfigOld, err := GetExporterScraperConfig(ctx, clientset, namespace, deploymentName)
	exporterScraperConfig := GetExporterScraperObject(namespace, groupKey, url, deploymentName)
	if err != nil || !checkExporterScraperConfigs(exporterScraperConfigOld, *exporterScraperConfig) {
		if groupKey != ">>" {
			DeleteExporterScraperConfig(ctx, clientset, namespace, deploymentName)
		}
		jsonData, err := json.Marshal(exporterScraperConfig)
		if err != nil {
			return err
		}
		response, err := clientset.RESTClient().Post().
			AbsPath("/apis/finops.krateo.io/v1").
			Namespace(namespace).
			Resource("exporterscraperconfigs").
			Name(deploymentName).
			Body(jsonData).
			DoRaw(ctx)
		if err != nil {
			fmt.Println(string(response))
			return err
		}
	}

	return nil
}

func GetExporterScraperObject(namespace string, groupKey string, url string, deploymentName string) *finopsDataTypes.ExporterScraperConfig {
	additionalVariables := make(map[string]string)
	additionalVariables["certFilePath"] = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	additionalVariables["kubernetes_host"] = "KUBERNETES_SERVICE_HOST"
	additionalVariables["kubernetes_port"] = "KUBERNETES_SERVICE_PORT"

	scaperConfigObject := finopsDataTypes.ScraperConfigSpec{}
	if groupKey != ">>" {
		scaperConfigObject = finopsDataTypes.ScraperConfigSpec{
			TableName:            strings.Split(groupKey, ">")[2],
			PollingIntervalHours: 6,
			ScraperDatabaseConfigRef: finopsDataTypes.ObjectRef{
				Name:      strings.Split(groupKey, ">")[1],
				Namespace: strings.Split(groupKey, ">")[0],
			},
		}
	}

	return &finopsDataTypes.ExporterScraperConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ExporterScraperConfig",
			APIVersion: "finops.krateo.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
		},
		Spec: finopsDataTypes.ExporterScraperConfigSpec{
			ExporterConfig: finopsDataTypes.ExporterConfigSpec{
				Provider:              finopsDataTypes.ObjectRef{},
				Url:                   url,
				RequireAuthentication: true,
				AuthenticationMethod:  "cert-file",
				MetricType:            "cost",
				PollingIntervalHours:  6,
				AdditionalVariables:   additionalVariables,
			},
			ScraperConfig: scaperConfigObject,
		},
	}
}

func GetClientSet() (*kubernetes.Clientset, error) {
	inClusterConfig, err := rest.InClusterConfig()
	if err != nil {
		return &kubernetes.Clientset{}, err
	}

	inClusterConfig.APIPath = "/apis"
	inClusterConfig.GroupVersion = &finopsv1.GroupVersion

	clientset, err := kubernetes.NewForConfig(inClusterConfig)
	if err != nil {
		return &kubernetes.Clientset{}, err
	}
	return clientset, nil
}

func DeleteExporterScraperConfig(ctx context.Context, clientset *kubernetes.Clientset, namespace string, deploymentName string) {
	_, _ = clientset.RESTClient().Delete().
		AbsPath("/apis/finops.krateo.io/v1").
		Namespace(namespace).
		Resource("exporterscraperconfigs").
		Name(deploymentName).
		DoRaw(ctx)
}

func GetExporterScraperConfig(ctx context.Context, clientset *kubernetes.Clientset, namespace string, deploymentName string) (finopsDataTypes.ExporterScraperConfig, error) {
	response, err := clientset.RESTClient().Get().
		AbsPath("/apis/finops.krateo.io/v1").
		Namespace(namespace).
		Resource("exporterscraperconfigs").
		Name(deploymentName).
		DoRaw(ctx)
	var exporterScraperConfig finopsDataTypes.ExporterScraperConfig
	if err != nil {
		return exporterScraperConfig, err
	} else {
		json.Unmarshal(response, &exporterScraperConfig)
		return exporterScraperConfig, nil
	}

}

func CreateGroupings(focusConfigList finopsv1.FocusConfigList) map[string][]finopsv1.FocusConfig {
	configGroupingByDatabase := make(map[string][]finopsv1.FocusConfig)
	for _, config := range focusConfigList.Items {
		newGroupKey := config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace +
			">" + config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name +
			">" + config.Spec.ScraperConfig.TableName
		arrSoFar := configGroupingByDatabase[newGroupKey]
		configGroupingByDatabase[newGroupKey] = append(arrSoFar, config)
	}
	return configGroupingByDatabase
}

func MakeGroupKeyKubeCompliant(groupKey string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(groupKey, "_", "-"), "=", "-"), ".", "-")
}

// Could probably be more readable and scalable with reflect, but for now its ok
func checkExporterScraperConfigs(exporterScraperConfig1 finopsDataTypes.ExporterScraperConfig, exporterScraperConfig2 finopsDataTypes.ExporterScraperConfig) bool {
	if exporterScraperConfig1.Kind != exporterScraperConfig2.Kind {
		return false
	}
	if exporterScraperConfig1.APIVersion != exporterScraperConfig2.APIVersion {
		return false
	}

	if exporterScraperConfig1.ObjectMeta.Name != exporterScraperConfig2.ObjectMeta.Name {
		return false
	}

	if exporterScraperConfig1.ObjectMeta.Namespace != exporterScraperConfig2.ObjectMeta.Namespace {
		return false
	}

	if exporterScraperConfig1.Spec.ExporterConfig.Provider.Name != exporterScraperConfig2.Spec.ExporterConfig.Provider.Name {
		return false
	}

	if exporterScraperConfig1.Spec.ExporterConfig.Provider.Namespace != exporterScraperConfig2.Spec.ExporterConfig.Provider.Namespace {
		return false
	}

	if exporterScraperConfig1.Spec.ExporterConfig.Url != exporterScraperConfig2.Spec.ExporterConfig.Url {
		return false
	}

	if exporterScraperConfig1.Spec.ExporterConfig.RequireAuthentication != exporterScraperConfig2.Spec.ExporterConfig.RequireAuthentication {
		return false
	}

	if exporterScraperConfig1.Spec.ExporterConfig.AuthenticationMethod != exporterScraperConfig2.Spec.ExporterConfig.AuthenticationMethod {
		return false
	}

	if exporterScraperConfig1.Spec.ExporterConfig.PollingIntervalHours != exporterScraperConfig2.Spec.ExporterConfig.PollingIntervalHours {
		return false
	}

	for key := range exporterScraperConfig1.Spec.ExporterConfig.AdditionalVariables {
		if exporterScraperConfig1.Spec.ExporterConfig.AdditionalVariables[key] != exporterScraperConfig2.Spec.ExporterConfig.AdditionalVariables[key] {
			return false
		}
	}

	if exporterScraperConfig1.Spec.ScraperConfig.TableName != exporterScraperConfig2.Spec.ScraperConfig.TableName {
		return false
	}

	if exporterScraperConfig1.Spec.ScraperConfig.PollingIntervalHours != exporterScraperConfig2.Spec.ScraperConfig.PollingIntervalHours {
		return false
	}

	if exporterScraperConfig1.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name != exporterScraperConfig2.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name {
		return false
	}

	if exporterScraperConfig1.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace != exporterScraperConfig2.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace {
		return false
	}

	return true
}
