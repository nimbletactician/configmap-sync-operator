// controllers/configmapsource_controller.go

package controllers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "example.com/configmap-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigMapSourceReconciler reconciles a ConfigMapSource object
type ConfigMapSourceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=config.example.com,resources=configmapsources,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.example.com,resources=configmapsources/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=config.example.com,resources=configmapsources/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile handles ConfigMapSource resources
func (r *ConfigMapSourceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling ConfigMapSource", "request", req.NamespacedName)

	// Fetch the ConfigMapSource instance
	var configMapSource configv1alpha1.ConfigMapSource
	if err := r.Get(ctx, req.NamespacedName, &configMapSource); err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request
			logger.Info("ConfigMapSource resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		logger.Error(err, "Failed to get ConfigMapSource")
		return ctrl.Result{}, err
	}

	// Add finalizer to handle cleanup when the resource is deleted
	if !controllerutil.ContainsFinalizer(&configMapSource, "configmapsource.config.example.com/finalizer") {
		controllerutil.AddFinalizer(&configMapSource, "configmapsource.config.example.com/finalizer")
		if err := r.Update(ctx, &configMapSource); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil // Requeue to continue processing
	}

	// Handle resource deletion
	if !configMapSource.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &configMapSource)
	}

	// Set target namespace if not specified
	targetNamespace := configMapSource.Spec.TargetNamespace
	if targetNamespace == "" {
		targetNamespace = configMapSource.Namespace
	}

	// Fetch configuration data from the source
	configData, err := r.fetchConfigData(ctx, &configMapSource)
	if err != nil {
		// Update status condition to reflect failure
		r.setStatusCondition(&configMapSource, metav1.Condition{
			Type:    "Ready",
			Status:  metav1.ConditionFalse,
			Reason:  "FetchFailed",
			Message: fmt.Sprintf("Failed to fetch configuration data: %v", err),
		})
		if updateErr := r.Status().Update(ctx, &configMapSource); updateErr != nil {
			logger.Error(updateErr, "Failed to update ConfigMapSource status after fetch failure")
		}
		logger.Error(err, "Failed to fetch configuration data")
		return ctrl.Result{RequeueAfter: time.Minute}, err // Retry after a minute
	}

	// Calculate hash of the config data for change detection
	configHash := calculateConfigHash(configData)

	// Check if the configuration has changed
	if configHash == configMapSource.Status.LastSyncHash {
		logger.Info("Configuration unchanged, no update needed")
		// Still update the status to reflect successful sync attempt
		now := metav1.Now()
		configMapSource.Status.LastSyncTime = &now
		if err := r.Status().Update(ctx, &configMapSource); err != nil {
			logger.Error(err, "Failed to update ConfigMapSource status for unchanged configuration")
			return ctrl.Result{}, err
		}

		// Requeue based on refresh interval
		return r.requeueBasedOnRefreshInterval(&configMapSource)
	}

	// Get the target ConfigMap if it exists
	var targetConfigMap corev1.ConfigMap
	targetConfigMapName := types.NamespacedName{
		Name:      configMapSource.Spec.TargetConfigMap,
		Namespace: targetNamespace,
	}
	configMapExists := true
	if err := r.Get(ctx, targetConfigMapName, &targetConfigMap); err != nil {
		if !apierrors.IsNotFound(err) {
			logger.Error(err, "Failed to get target ConfigMap")
			return ctrl.Result{}, err
		}
		configMapExists = false

		// Initialize new ConfigMap if it doesn't exist
		targetConfigMap = corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapSource.Spec.TargetConfigMap,
				Namespace: targetNamespace,
			},
			Data: make(map[string]string),
		}
	}

	// Update or create the target ConfigMap
	if configMapExists {
		logger.Info("Updating existing ConfigMap", "name", targetConfigMapName)
		targetConfigMap.Data = configData
		if err := r.Update(ctx, &targetConfigMap); err != nil {
			logger.Error(err, "Failed to update ConfigMap")
			return ctrl.Result{}, err
		}
	} else {
		logger.Info("Creating new ConfigMap", "name", targetConfigMapName)
		targetConfigMap.Data = configData

		// Set owner reference if in the same namespace
		if configMapSource.Namespace == targetNamespace {
			if err := controllerutil.SetControllerReference(&configMapSource, &targetConfigMap, r.Scheme); err != nil {
				logger.Error(err, "Failed to set owner reference on ConfigMap")
				return ctrl.Result{}, err
			}
		}

		if err := r.Create(ctx, &targetConfigMap); err != nil {
			logger.Error(err, "Failed to create ConfigMap")
			return ctrl.Result{}, err
		}
	}

	// Update status with sync info
	now := metav1.Now()
	configMapSource.Status.LastSyncTime = &now
	configMapSource.Status.LastSyncHash = configHash
	r.setStatusCondition(&configMapSource, metav1.Condition{
		Type:    "Ready",
		Status:  metav1.ConditionTrue,
		Reason:  "SyncSuccess",
		Message: "Successfully synced configuration data",
	})
	if err := r.Status().Update(ctx, &configMapSource); err != nil {
		logger.Error(err, "Failed to update ConfigMapSource status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled ConfigMapSource", "name", req.NamespacedName)

	// Requeue based on refresh interval
	return r.requeueBasedOnRefreshInterval(&configMapSource)
}

// reconcileDelete handles the deletion of a ConfigMapSource resource
func (r *ConfigMapSourceReconciler) reconcileDelete(ctx context.Context, configMapSource *configv1alpha1.ConfigMapSource) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Deleting ConfigMapSource", "name", configMapSource.Name)

	// Determine the target namespace
	targetNamespace := configMapSource.Spec.TargetNamespace
	if targetNamespace == "" {
		targetNamespace = configMapSource.Namespace
	}

	// Remove the target ConfigMap if we own it
	if configMapSource.Namespace == targetNamespace {
		var targetConfigMap corev1.ConfigMap
		targetConfigMapName := types.NamespacedName{
			Name:      configMapSource.Spec.TargetConfigMap,
			Namespace: targetNamespace,
		}

		if err := r.Get(ctx, targetConfigMapName, &targetConfigMap); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "Failed to get target ConfigMap during deletion")
				return ctrl.Result{}, err
			}
			// ConfigMap already deleted, continue with finalizer removal
		} else {
			// Check if this ConfigMapSource is the owner
			if isOwnedBy(&targetConfigMap, configMapSource) {
				logger.Info("Deleting owned ConfigMap", "name", targetConfigMapName)
				if err := r.Delete(ctx, &targetConfigMap); err != nil {
					if !apierrors.IsNotFound(err) {
						logger.Error(err, "Failed to delete ConfigMap")
						return ctrl.Result{}, err
					}
				}
			}
		}
	}

	// Remove finalizer to allow deletion
	controllerutil.RemoveFinalizer(configMapSource, "configmapsource.config.example.com/finalizer")
	if err := r.Update(ctx, configMapSource); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully finalized ConfigMapSource", "name", configMapSource.Name)
	return ctrl.Result{}, nil
}

// fetchConfigData retrieves configuration data from the specified source
func (r *ConfigMapSourceReconciler) fetchConfigData(ctx context.Context, configMapSource *configv1alpha1.ConfigMapSource) (map[string]string, error) {
	switch configMapSource.Spec.SourceType {
	case "Git":
		return r.fetchFromGit(ctx, configMapSource)
	case "File":
		return r.fetchFromFile(ctx, configMapSource)
	case "ConfigMap":
		return r.fetchFromConfigMap(ctx, configMapSource)
	case "Secret":
		return r.fetchFromSecret(ctx, configMapSource)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", configMapSource.Spec.SourceType)
	}
}

// fetchFromGit retrieves configuration data from a Git repository
func (r *ConfigMapSourceReconciler) fetchFromGit(ctx context.Context, configMapSource *configv1alpha1.ConfigMapSource) (map[string]string, error) {
	logger := log.FromContext(ctx)
	if configMapSource.Spec.Git == nil {
		return nil, fmt.Errorf("Git source configuration is missing")
	}

	// Create temporary directory for Git clone
	tempDir, err := ioutil.TempDir("", "git-config-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	logger.Info("Cloning Git repository", "url", configMapSource.Spec.Git.URL, "revision", configMapSource.Spec.Git.Revision)

	// Setup authentication if needed
	var auth *ssh.PublicKeys
	if configMapSource.Spec.Git.AuthSecretRef != nil {
		// Get auth secret
		secretNamespace := configMapSource.Spec.Git.AuthSecretRef.Namespace
		if secretNamespace == "" {
			secretNamespace = configMapSource.Namespace
		}

		var secret corev1.Secret
		secretName := types.NamespacedName{
			Name:      configMapSource.Spec.Git.AuthSecretRef.Name,
			Namespace: secretNamespace,
		}

		if err := r.Get(ctx, secretName, &secret); err != nil {
			return nil, fmt.Errorf("failed to get auth secret: %w", err)
		}

		// Get SSH key from secret
		sshKeyData, ok := secret.Data[configMapSource.Spec.Git.AuthSecretRef.Key]
		if !ok {
			return nil, fmt.Errorf("SSH key not found in secret at key: %s", configMapSource.Spec.Git.AuthSecretRef.Key)
		}

		// Create SSH auth from private key
		auth, err = ssh.NewPublicKeysFromFile("git", "", "")
		if err != nil {
			return nil, fmt.Errorf("failed to create SSH auth: %w", err)
		}
		auth.Signer, err = ssh.NewSignerFromSigner(sshKeyData)
		if err != nil {
			return nil, fmt.Errorf("failed to create signer from SSH key: %w", err)
		}
	}

	// Clone options
	cloneOptions := &git.CloneOptions{
		URL:           configMapSource.Spec.Git.URL,
		SingleBranch:  true,
		ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", configMapSource.Spec.Git.Revision)),
		Auth:          auth,
		Depth:         1,
	}

	// Clone repository
	_, err = git.PlainClone(tempDir, false, cloneOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	// Get configuration files from path
	configPath := filepath.Join(tempDir, configMapSource.Spec.Git.Path)
	return readConfigFiles(configPath)
}

// fetchFromFile retrieves configuration data from a local file
func (r *ConfigMapSourceReconciler) fetchFromFile(ctx context.Context, configMapSource *configv1alpha1.ConfigMapSource) (map[string]string, error) {
	logger := log.FromContext(ctx)
	if configMapSource.Spec.File == nil {
		return nil, fmt.Errorf("File source configuration is missing")
	}

	logger.Info("Reading configuration from file", "path", configMapSource.Spec.File.Path)
	return readConfigFiles(configMapSource.Spec.File.Path)
}

// fetchFromConfigMap retrieves configuration data from another ConfigMap
func (r *ConfigMapSourceReconciler) fetchFromConfigMap(ctx context.Context, configMapSource *configv1alpha1.ConfigMapSource) (map[string]string, error) {
	logger := log.FromContext(ctx)
	if configMapSource.Spec.ConfigMap == nil {
		return nil, fmt.Errorf("ConfigMap source configuration is missing")
	}

	// Determine source namespace
	sourceNamespace := configMapSource.Spec.ConfigMap.Namespace
	if sourceNamespace == "" {
		sourceNamespace = configMapSource.Namespace
	}

	// Fetch source ConfigMap
	var sourceConfigMap corev1.ConfigMap
	sourceConfigMapName := types.NamespacedName{
		Name:      configMapSource.Spec.ConfigMap.Name,
		Namespace: sourceNamespace,
	}

	logger.Info("Fetching configuration from ConfigMap", "name", sourceConfigMapName)
	if err := r.Get(ctx, sourceConfigMapName, &sourceConfigMap); err != nil {
		return nil, fmt.Errorf("failed to get source ConfigMap: %w", err)
	}

	// Filter keys if specified
	resultData := make(map[string]string)
	if len(configMapSource.Spec.ConfigMap.Keys) > 0 {
		for _, key := range configMapSource.Spec.ConfigMap.Keys {
			if value, exists := sourceConfigMap.Data[key]; exists {
				resultData[key] = value
			}
		}
	} else {
		// Copy all keys
		for key, value := range sourceConfigMap.Data {
			resultData[key] = value
		}
	}

	return resultData, nil
}

// fetchFromSecret retrieves configuration data from a Secret
func (r *ConfigMapSourceReconciler) fetchFromSecret(ctx context.Context, configMapSource *configv1alpha1.ConfigMapSource) (map[string]string, error) {
	logger := log.FromContext(ctx)
	if configMapSource.Spec.Secret == nil {
		return nil, fmt.Errorf("Secret source configuration is missing")
	}

	// Determine source namespace
	sourceNamespace := configMapSource.Spec.Secret.Namespace
	if sourceNamespace == "" {
		sourceNamespace = configMapSource.Namespace
	}

	// Fetch source Secret
	var sourceSecret corev1.Secret
	sourceSecretName := types.NamespacedName{
		Name:      configMapSource.Spec.Secret.Name,
		Namespace: sourceNamespace,
	}

	logger.Info("Fetching configuration from Secret", "name", sourceSecretName)
	if err := r.Get(ctx, sourceSecretName, &sourceSecret); err != nil {
		return nil, fmt.Errorf("failed to get source Secret: %w", err)
	}

	// Filter keys if specified
	resultData := make(map[string]string)
	if len(configMapSource.Spec.Secret.Keys) > 0 {
		for _, key := range configMapSource.Spec.Secret.Keys {
			if value, exists := sourceSecret.Data[key]; exists {
				resultData[key] = string(value)
			}
		}
	} else {
		// Copy all keys
		for key, value := range sourceSecret.Data {
			resultData[key] = string(value)
		}
	}

	return resultData, nil
}

// readConfigFiles reads configuration files from a directory
func readConfigFiles(path string) (map[string]string, error) {
	configData := make(map[string]string)

	fileInfo, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	if fileInfo.IsDir() {
		// Read all files in directory
		files, err := ioutil.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read directory: %w", err)
		}

		for _, file := range files {
			if file.IsDir() {
				continue // Skip subdirectories
			}

			filePath := filepath.Join(path, file.Name())
			content, err := ioutil.ReadFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
			}

			configData[file.Name()] = string(content)
		}
	} else {
		// Read single file
		content, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}

		// Use base filename as the key
		fileName := filepath.Base(path)
		configData[fileName] = string(content)
	}

	return configData, nil
}

// calculateConfigHash creates a hash of the configuration data for change detection
func calculateConfigHash(configData map[string]string) string {
	hash := sha256.New()

	// Sort keys for consistent hashing
	keys := make([]string, 0, len(configData))
	for k := range configData {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Hash each key-value pair
	for _, key := range keys {
		hash.Write([]byte(key))
		hash.Write([]byte(configData[key]))
	}

	return hex.EncodeToString(hash.Sum(nil))
}

// requeueBasedOnRefreshInterval determines when to requeue based on refresh interval
func (r *ConfigMapSourceReconciler) requeueBasedOnRefreshInterval(configMapSource *configv1alpha1.ConfigMapSource) (ctrl.Result, error) {
	if configMapSource.Spec.RefreshInterval != nil && *configMapSource.Spec.RefreshInterval > 0 {
		interval := time.Duration(*configMapSource.Spec.RefreshInterval) * time.Second
		return ctrl.Result{RequeueAfter: interval}, nil
	}

	// No automatic refresh
	return ctrl.Result{}, nil
}

// setStatusCondition updates a status condition
func (r *ConfigMapSourceReconciler) setStatusCondition(configMapSource *configv1alpha1.ConfigMapSource, condition metav1.Condition) {
	// Initialize conditions if nil
	if configMapSource.Status.Conditions == nil {
		configMapSource.Status.Conditions = []metav1.Condition{}
	}

	// Find existing condition
	conditionIndex := -1
	for i, cond := range configMapSource.Status.Conditions {
		if cond.Type == condition.Type {
			conditionIndex = i
			break
		}
	}

	// Set condition
	condition.LastTransitionTime = metav1.Now()
	condition.ObservedGeneration = configMapSource.Generation

	if conditionIndex != -1 {
		// Update existing condition
		configMapSource.Status.Conditions[conditionIndex] = condition
	} else {
		// Add new condition
		configMapSource.Status.Conditions = append(configMapSource.Status.Conditions, condition)
	}
}

// isOwnedBy checks if a ConfigMap is owned by a ConfigMapSource
func isOwnedBy(obj *corev1.ConfigMap, owner *configv1alpha1.ConfigMapSource) bool {
	for _, ref := range obj.OwnerReferences {
		if ref.UID == owner.UID {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager
func (r *ConfigMapSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.ConfigMapSource{}).
		Owns(&corev1.ConfigMap{}).
		Complete(r)
}
