package client

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	finopsDataTypes "github.com/krateoplatformops/finops-data-types/api/v1"
	"github.com/rs/zerolog/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func New(rc *rest.Config) (*dynamic.DynamicClient, error) {
	config := *rc
	config.APIPath = "/api"
	config.NegotiatedSerializer = serializer.NewCodecFactory(scheme.Scheme)
	config.UserAgent = rest.DefaultKubernetesUserAgent()
	//config.QPS = 1000
	//config.Burst = 3000

	return dynamic.NewForConfig(&config)
}

func GetObj(ctx context.Context, cr *finopsDataTypes.ObjectRef, ApiVersion string, Resource string, dynClient *dynamic.DynamicClient) (*unstructured.Unstructured, error) {
	gv, err := schema.ParseGroupVersion(ApiVersion)
	if err != nil {
		return nil, fmt.Errorf("unable to parse GroupVersion from composition reference ApiVersion: %w", err)
	}
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: Resource,
	}
	res, err := dynClient.Resource(gvr).Namespace(cr.Namespace).Get(ctx, cr.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve resource %s with name %s in namespace %s, with apiVersion %s: %w", Resource, cr.Name, cr.Namespace, ApiVersion, err)
	}
	return res, nil
}

func ListObj(ctx context.Context, Namespace string, ApiVersion string, Resource string, dynClient *dynamic.DynamicClient) (*unstructured.UnstructuredList, error) {
	gv, err := schema.ParseGroupVersion(ApiVersion)
	if err != nil {
		return nil, fmt.Errorf("unable to parse GroupVersion from composition reference ApiVersion: %w", err)
	}
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: Resource,
	}
	res, err := dynClient.Resource(gvr).Namespace(Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to list resource %s in namespace %s with apiVersion %s: %w", Resource, Namespace, ApiVersion, err)
	}
	return res, nil

}

func UpdateObj(ctx context.Context, objToUpdate *unstructured.Unstructured, Resource string, dynClient *dynamic.DynamicClient) error {
	gv, _ := schema.ParseGroupVersion(objToUpdate.GetAPIVersion())
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: Resource,
	}
	_, err := dynClient.Resource(gvr).Namespace(objToUpdate.GetNamespace()).Update(ctx, objToUpdate, metav1.UpdateOptions{})
	return err
}

func UpdateStatus(ctx context.Context, objToUpdate *unstructured.Unstructured, Resource string, dynClient *dynamic.DynamicClient) error {
	gv, _ := schema.ParseGroupVersion(objToUpdate.GetAPIVersion())
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: Resource,
	}
	_, err := dynClient.Resource(gvr).Namespace(objToUpdate.GetNamespace()).UpdateStatus(ctx, objToUpdate, metav1.UpdateOptions{})
	return err
}

func CreateObj(ctx context.Context, objToUpdate *unstructured.Unstructured, Resource string, dynClient *dynamic.DynamicClient) error {
	gv, _ := schema.ParseGroupVersion(objToUpdate.GetAPIVersion())
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: Resource,
	}
	log.Logger.Debug().Msgf("create request for %s %s %s %s %s", objToUpdate.GetName(), objToUpdate.GetNamespace(), gvr.Group, gvr.Version, gvr.Resource)
	_, err := dynClient.Resource(gvr).Namespace(objToUpdate.GetNamespace()).Create(ctx, objToUpdate, metav1.CreateOptions{})
	return err
}

func DeleteObj(ctx context.Context, cr *finopsDataTypes.ObjectRef, ApiVersion string, Resource string, dynClient *dynamic.DynamicClient) error {
	gv, _ := schema.ParseGroupVersion(ApiVersion)
	gvr := schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: Resource,
	}
	return dynClient.Resource(gvr).Namespace(cr.Namespace).Delete(ctx, cr.Name, metav1.DeleteOptions{})
}

func ToUnstructured(obj runtime.Object) (*unstructured.Unstructured, error) {
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %v", err)
	}
	return &unstructured.Unstructured{Object: data}, nil
}
