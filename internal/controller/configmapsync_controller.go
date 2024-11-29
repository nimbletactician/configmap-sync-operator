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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	syncv1alpha1 "github.com/example/configmap-sync-operator/api/v1alpha1"
)

// ConfigMapSyncReconciler reconciles a ConfigMapSync object
type ConfigMapSyncReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=sync.example.com,resources=configmapsyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=sync.example.com,resources=configmapsyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sync.example.com,resources=configmapsyncs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ConfigMapSync object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.4/pkg/reconcile
func (r *ConfigMapSyncReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the ConfigMapSync instance
	var configMapSync syncv1alpha1.ConfigMapSync
	if err := r.Get(ctx, req.NamespacedName, &configMapSync); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the source ConfigMap
	var sourceConfigMap corev1.ConfigMap
	if err := r.Get(ctx, types.NamespacedName{
		Name:      configMapSync.Spec.SourceConfigMap,
		Namespace: configMapSync.Namespace,
	}, &sourceConfigMap); err != nil {
		if errors.IsNotFound(err) {
			// Source ConfigMap doesn't exist yet, requeue
			return ctrl.Result{RequeueAfter: time.Second * 10}, nil
		}
		return ctrl.Result{}, err
	}

	// Track which namespaces we've successfully synced to
	syncedNamespaces := []string{}

	// Sync to each target namespace
	for _, namespace := range configMapSync.Spec.TargetNameSpaces {
		// Skip if target namespace is the same as source namespace
		if namespace == configMapSync.Namespace {
			continue
		}

		// Create or update ConfigMap in target namespace
		targetConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sourceConfigMap.Name,
				Namespace: namespace,
				Labels:    configMapSync.Spec.Labels,
			},
		}

		// Set controller reference if target is in same namespace
		if namespace == configMapSync.Namespace {
			if err := controllerutil.SetControllerReference(&configMapSync, targetConfigMap, r.Scheme); err != nil {
				log.Error(err, "unable to set controller reference")
				continue
			}
		}

		// Copy data from source ConfigMap
		targetConfigMap.Data = sourceConfigMap.Data
		targetConfigMap.BinaryData = sourceConfigMap.BinaryData

		// Create or update the target ConfigMap
		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, targetConfigMap, func() error {
			targetConfigMap.Data = sourceConfigMap.Data
			targetConfigMap.BinaryData = sourceConfigMap.BinaryData
			return nil
		})

		if err != nil {
			log.Error(err, "failed to sync ConfigMap", "namespace", namespace)
			continue
		}

		syncedNamespaces = append(syncedNamespaces, namespace)
	}

	// Update status
	now := metav1.NewTime(time.Now())
	configMapSync.Status.LastSyncTime = &now
	configMapSync.Status.SyncedNamespaces = syncedNamespaces

	if err := r.Status().Update(ctx, &configMapSync); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue periodically to check for changes
	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigMapSyncReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&syncv1alpha1.ConfigMapSync{}).
		Complete(r)
}
