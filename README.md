# ConfigMap Sync Operator - Controller Walkthrough

This document provides a line-by-line walkthrough of the ConfigMap Sync Operator's controller logic.

## ConfigMapSourceReconciler Struct

The controller uses a standard Kubernetes controller struct with client, scheme, and logger:

```go
type ConfigMapSourceReconciler struct {
    client.Client
    Scheme *runtime.Scheme
    Log    logr.Logger
}
```

## Main Reconciliation Function

The `Reconcile` function is called whenever a ConfigMapSource resource is created, updated, or deleted.

### Step 1: Initial Setup
```go
// Get logger and log reconciliation start
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
```

### Step 2: Finalizer Management
```go
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
```

### Step 3: Target Namespace Resolution
```go
// Set target namespace if not specified
targetNamespace := configMapSource.Spec.TargetNamespace
if targetNamespace == "" {
    targetNamespace = configMapSource.Namespace
}
```

### Step 4: Data Fetching
```go
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
```

### Step 5: Change Detection
```go
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
```

### Step 6: Target ConfigMap Management
```go
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
```

### Step 7: Update or Create ConfigMap
```go
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
```

### Step 8: Status Update
```go
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
```

### Step 9: Requeue for Periodic Updates
```go
// Requeue based on refresh interval
return r.requeueBasedOnRefreshInterval(&configMapSource)
```

## Deletion Handling

The `reconcileDelete` function handles cleanup when a ConfigMapSource is being deleted:

```go
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
```

## Data Fetching from Sources

The controller supports multiple source types, with a dispatcher function to route to the appropriate handler:

```go
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
```

### Git Source Handler

Fetches configuration from Git repositories:

1. Creates a temporary directory
2. Sets up SSH authentication if needed
3. Clones the repository
4. Reads configuration files from specified path

### File Source Handler

Reads configuration from a local file or directory path.

### ConfigMap Source Handler

Copies data from another ConfigMap:
1. Determines source namespace
2. Fetches source ConfigMap
3. Filters keys if specified or copies all data

### Secret Source Handler

Copies data from a Secret:
1. Determines source namespace
2. Fetches source Secret
3. Filters keys if specified or copies all data
4. Converts binary data to string

## Utility Functions

### readConfigFiles

Reads configuration files from a path:
```go
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
```

### calculateConfigHash

Generates a hash for change detection:
```go
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
```

### requeueBasedOnRefreshInterval

Determines when to requeue for periodic updates:
```go
func (r *ConfigMapSourceReconciler) requeueBasedOnRefreshInterval(configMapSource *configv1alpha1.ConfigMapSource) (ctrl.Result, error) {
    if configMapSource.Spec.RefreshInterval != nil && *configMapSource.Spec.RefreshInterval > 0 {
        interval := time.Duration(*configMapSource.Spec.RefreshInterval) * time.Second
        return ctrl.Result{RequeueAfter: interval}, nil
    }

    // No automatic refresh
    return ctrl.Result{}, nil
}
```

### setStatusCondition

Updates status conditions:
```go
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
```

## Controller Setup

The `SetupWithManager` function registers the controller with the controller-runtime:

```go
func (r *ConfigMapSourceReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&configv1alpha1.ConfigMapSource{}).
        Owns(&corev1.ConfigMap{}).
        Complete(r)
}
```

This configures the controller to:
1. Watch for changes to ConfigMapSource resources
2. Watch for changes to owned ConfigMap resources
3. Trigger reconciliation when these resources change
