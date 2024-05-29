package utils

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	operatorPackage "github.com/krateoplatformops/finops-operator-exporter/api/v1"
	finopsv1 "github.com/krateoplatformops/finops-operator-focus/api/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var repository = strings.TrimSuffix(os.Getenv("REPO"), "/")

func Int32Ptr(i int32) *int32 { return &i }

func GetGenericWebserviceDeployment(focusConfig finopsv1.FocusConfig) (*appsv1.Deployment, error) {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      focusConfig.Name + "-webservice-generic-deployment",
			Namespace: focusConfig.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: focusConfig.APIVersion,
					Kind:       focusConfig.Kind,
					Name:       focusConfig.Name,
					UID:        focusConfig.UID,
				},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: Int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"scraper": focusConfig.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"scraper": focusConfig.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "scraper",
							Image:           repository + "/finops-webservice-api-generic:latest",
							ImagePullPolicy: corev1.PullAlways,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config-volume",
									MountPath: "/temp",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "focus-" + focusConfig.Status.GroupKey + "-configmap",
									},
								},
							},
						},
					},
					ImagePullSecrets: []corev1.LocalObjectReference{
						{
							Name: "registry-credentials-default",
						},
					},
				},
			},
		},
	}, nil
}

func GetGenericWebserviceConfigMap(output string, focusConfig finopsv1.FocusConfig) (*corev1.ConfigMap, error) {
	binaryData := make(map[string][]byte)
	binaryData[focusConfig.Status.GroupKey+".dat"] = []byte(output)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "focus-" + focusConfig.Status.GroupKey + "-configmap",
			Namespace: focusConfig.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: focusConfig.APIVersion,
					Kind:       focusConfig.Kind,
					Name:       focusConfig.Name,
					UID:        focusConfig.UID,
				},
			},
		},
		BinaryData: binaryData,
	}, nil
}

func GetStringValue(value any) string {

	str, ok := value.(string)
	if ok {
		return str
	}

	integer, ok := value.(int)
	if ok {
		return strconv.FormatInt(int64(integer), 10)
	}

	integer64, ok := value.(int64)
	if ok {
		return strconv.FormatInt(integer64, 10)
	}

	resourceQuantity, ok := value.(resource.Quantity)
	if ok {
		return resourceQuantity.AsDec().String()
	}

	metav1Time, ok := value.(metav1.Time)
	if ok {
		return metav1Time.Format(time.RFC3339)
	}

	tags, ok := value.([]finopsv1.TagsType)
	if ok {
		res := ""
		for _, tag := range tags {
			res += tag.Key + "=" + tag.Value + ";"
		}
		return strings.TrimSuffix(res, ";")
	}

	return ""
}

func GetGenericWebserviceService(focusConfig finopsv1.FocusConfig) (*corev1.Service, error) {
	labels := make(map[string]string)
	labels["scraper"] = focusConfig.Name
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      focusConfig.Name + "-service",
			Namespace: focusConfig.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: focusConfig.APIVersion,
					Kind:       focusConfig.Kind,
					Name:       focusConfig.Name,
					UID:        focusConfig.UID,
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Type:     corev1.ServiceTypeNodePort,
			Ports: []corev1.ServicePort{
				{
					Protocol: corev1.ProtocolTCP,
					Port:     8080,
				},
			},
		},
	}, nil
}

func CreateExporterCR(ctx context.Context, focusConfig finopsv1.FocusConfig, serviceIp string, servicePort int) error {
	if focusConfig.Spec.ScraperConfig.TableName == "" &&
		focusConfig.Spec.ScraperConfig.PollingIntervalHours == 0 &&
		focusConfig.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name == "" &&
		focusConfig.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace == "" {
		return nil
	}
	clientset, err := GetClientSet()
	if err != nil {
		return err
	}
	DeleteExporterScraperConfig(ctx, focusConfig, clientset)
	for CheckExporterScraperConfigDeletion(ctx, focusConfig, clientset) {
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(500 * time.Millisecond)
	url := "http://" + serviceIp + ":" + strconv.FormatInt(int64(servicePort), 10) + "/data/" + focusConfig.Status.GroupKey
	exporterScraperConfig := GetExporterScraperObject(focusConfig, serviceIp, servicePort, url)
	jsonData, err := json.Marshal(exporterScraperConfig)
	if err != nil {
		return err
	}
	_, err = clientset.RESTClient().Post().
		AbsPath("/apis/finops.krateo.io/v1").
		Namespace(focusConfig.Namespace).
		Resource("exporterscraperconfigs").
		Name(focusConfig.Name + "-exporter").
		Body(jsonData).
		DoRaw(ctx)
	if err != nil {
		return err
	}

	return nil
}

func GetExporterScraperObject(focusConfig finopsv1.FocusConfig, serviceIp string, servicePort int, url string) *operatorPackage.ExporterScraperConfig {
	return &operatorPackage.ExporterScraperConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ExporterScraperConfig",
			APIVersion: "finops.krateo.io/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      focusConfig.Name + "-exporter",
			Namespace: focusConfig.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: focusConfig.APIVersion,
					Kind:       focusConfig.Kind,
					Name:       focusConfig.Name,
					UID:        focusConfig.UID,
				},
			},
		},
		Spec: operatorPackage.ExporterScraperConfigSpec{
			ExporterConfig: operatorPackage.ExporterConfig{
				Name:                  focusConfig.Name,
				Url:                   url,
				RequireAuthentication: false,
				PollingIntervalHours:  focusConfig.Spec.ScraperConfig.PollingIntervalHours,
				AdditionalVariables:   make(map[string]string),
			},
			ScraperConfig: operatorPackage.ScraperConfig{
				TableName:            focusConfig.Spec.ScraperConfig.TableName,
				PollingIntervalHours: focusConfig.Spec.ScraperConfig.PollingIntervalHours,
				ScraperDatabaseConfigRef: operatorPackage.ScraperDatabaseConfigRef{
					Name:      focusConfig.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name,
					Namespace: focusConfig.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace,
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

func DeleteExporterScraperConfig(ctx context.Context, focusConfig finopsv1.FocusConfig, clientset *kubernetes.Clientset) {
	_, _ = clientset.RESTClient().Delete().
		AbsPath("/apis/finops.krateo.io/v1").
		Namespace(focusConfig.Namespace).
		Resource("exporterscraperconfigs").
		Name(focusConfig.Name + "-exporter").
		DoRaw(ctx)
}

func CheckExporterScraperConfigDeletion(ctx context.Context, focusConfig finopsv1.FocusConfig, clientset *kubernetes.Clientset) bool {
	_, err := clientset.RESTClient().Get().
		AbsPath("/apis/finops.krateo.io/v1").
		Namespace(focusConfig.Namespace).
		Resource("exporterscraperconfigs").
		Name(focusConfig.Name + "-exporter").
		DoRaw(ctx)
	return err == nil
}

func CreateGroupings(focusConfigList finopsv1.FocusConfigList) map[string][]finopsv1.FocusConfig {
	configGroupingByDatabase := make(map[string][]finopsv1.FocusConfig)
	for _, config := range focusConfigList.Items {
		newGroupKey := strings.ReplaceAll(config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace+
			"-"+config.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name+
			"-"+config.Spec.ScraperConfig.TableName, "_", "-")
		arrSoFar := configGroupingByDatabase[newGroupKey]
		configGroupingByDatabase[newGroupKey] = append(arrSoFar, config)
	}
	return configGroupingByDatabase
}

func GetOutputStr(configGroupingByDatabase map[string][]finopsv1.FocusConfig, groupKey string) (finopsv1.FocusConfig, string) {
	outputStr := ""
	var focusConfigRoot finopsv1.FocusConfig
	for i, config := range configGroupingByDatabase[groupKey] {
		v := reflect.ValueOf(config.Spec.FocusSpec)

		if config.Status.FocusConfigRoot == config.Name {
			focusConfigRoot = *config.DeepCopy()
		}
		if i == 0 {
			for i := 0; i < v.NumField(); i++ {
				outputStr += v.Type().Field(i).Name + ","
			}
			outputStr = strings.TrimSuffix(outputStr, ",") + "\n"
		}

		for i := 0; i < v.NumField(); i++ {
			outputStr += GetStringValue(v.Field(i).Interface()) + ","
		}
		outputStr = strings.TrimSuffix(outputStr, ",") + "\n"
	}
	outputStr = strings.TrimSuffix(outputStr, "\n")

	return focusConfigRoot, outputStr
}

func AssignRootElementIfNotPresent(configGroupingByDatabase map[string][]finopsv1.FocusConfig) (map[string][]finopsv1.FocusConfig, string) {
	keyWithoutRoot := ""
	for key := range configGroupingByDatabase {
		rootFound := false
		for _, config := range configGroupingByDatabase[key] {
			if config.Status.FocusConfigRoot == config.Name {
				rootFound = true
			}
		}
		if !rootFound {
			keyWithoutRoot = key
			for i := range configGroupingByDatabase[key] {
				configGroupingByDatabase[key][i].Status.FocusConfigRoot = configGroupingByDatabase[key][0].Name
			}
		}
	}
	return configGroupingByDatabase, keyWithoutRoot
}
