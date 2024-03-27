package utils

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	finopsv1 "operator-focus/api/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var repository = os.Getenv("REPO")

func Int32Ptr(i int32) *int32 { return &i }

func GetGenericExporterDeployment(focusConfig finopsv1.FocusConfig) (*appsv1.Deployment, error) {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      focusConfig.Name + "-deployment",
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
							Image:           repository + "/finops-prometheus-exporter-generic:0.1.0",
							Args:            []string{focusConfig.Name},
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
										Name: focusConfig.Name + "-configmap",
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

func GetGenericExporterConfigMap(output string, focusConfig finopsv1.FocusConfig) (*corev1.ConfigMap, error) {
	binaryData := make(map[string][]byte)
	binaryData[focusConfig.Name+".dat"] = []byte(output)
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      focusConfig.Name + "-configmap",
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

func GetGenericExporterService(focusConfig finopsv1.FocusConfig) (*corev1.Service, error) {
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
					Port:     2112,
				},
			},
		},
	}, nil
}

func CreateScraperCR(ctx context.Context, focusConfig finopsv1.FocusConfig, serviceIp string, servicePort int) error {
	if focusConfig.Spec.ScraperConfig.TableName == "" &&
		focusConfig.Spec.ScraperConfig.PollingIntervalHours == 0 &&
		focusConfig.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name == "" &&
		focusConfig.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace == "" {
		return nil
	}
	inClusterConfig, err := rest.InClusterConfig()
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(inClusterConfig)
	if err != nil {
		return err
	}

	jsonData, _ := clientset.RESTClient().Get().
		AbsPath("/apis/finops.krateo.io/v1").
		Namespace(focusConfig.Namespace).
		Resource("scraperconfigs").
		Name(focusConfig.Name + "-scraper").
		DoRaw(context.TODO())

	var crdResponse CRDResponse
	_ = json.Unmarshal(jsonData, &crdResponse)
	if crdResponse.Status == "Failure" {
		url := focusConfig.Spec.ScraperConfig.Url
		if url == "" {
			url = "http://" + serviceIp + ":" + strconv.FormatInt(int64(servicePort), 10) + "/metrics"
		}
		scraperConfig := &finopsv1.ScraperConfigFromScraperOperator{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ScraperConfig",
				APIVersion: "finops.krateo.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      focusConfig.Name + "-scraper",
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
			Spec: finopsv1.ScraperConfigSpecFromScraperOperator{
				TableName:            focusConfig.Spec.ScraperConfig.TableName,
				Url:                  url,
				PollingIntervalHours: focusConfig.Spec.ScraperConfig.PollingIntervalHours,
				ScraperDatabaseConfigRef: finopsv1.ScraperDatabaseConfigRef{
					Name:      focusConfig.Spec.ScraperConfig.ScraperDatabaseConfigRef.Name,
					Namespace: focusConfig.Spec.ScraperConfig.ScraperDatabaseConfigRef.Namespace,
				},
			},
		}
		jsonData, err = json.Marshal(scraperConfig)
		if err != nil {
			return err
		}
		_, err := clientset.RESTClient().Post().
			AbsPath("/apis/finops.krateo.io/v1").
			Namespace(focusConfig.Namespace).
			Resource("scraperconfigs").
			Name(focusConfig.Name).
			Body(jsonData).
			DoRaw(ctx)

		if err != nil {
			return err
		}
	}
	return nil
}

type CRDResponse struct {
	Status string `json:"status"`
}
