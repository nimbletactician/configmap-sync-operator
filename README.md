- The ConfigMapSourceReconciler struct is defined with the Kubernetes client, scheme, and logger.

  Lines 46-189: Reconcile function

  This is the main reconciliation function, executed when ConfigMapSource resources change:

  1. Line 48-49: Gets a logger and logs reconciliation start
  2. Lines 52-62: Fetches the ConfigMapSource instance
    - If not found (deleted), returns with no error
    - If other error, returns with error to requeue
  3. Lines 64-72: Adds a finalizer
    - Checks if finalizer exists, adds if needed
    - Updates the resource and requeues for further processing
  4. Lines 74-77: Handles deletion
    - If deletion timestamp is set, calls reconcileDelete
  5. Lines 79-83: Sets target namespace
    - Uses specified target namespace or defaults to same namespace
  6. Lines 85-100: Fetches configuration data
    - Calls fetchConfigData to get data from the source
    - If fetch fails, updates status condition to reflect failure
    - Returns with error and requeues after one minute
  7. Lines 102-118: Checks if configuration changed
    - Calculates hash of configuration data
    - If hash matches last sync hash, updates only timestamp and requeues
    - No data update when unchanged to avoid unnecessary updates
  8. Lines 120-142: Gets or initializes target ConfigMap
    - Tries to get existing ConfigMap
    - If not found, initializes a new empty ConfigMap
  9. Lines 144-168: Updates or creates target ConfigMap
    - For existing ConfigMap: updates data, calls Update
    - For new ConfigMap:
        - Sets data
      - Sets owner reference if in same namespace
      - Calls Create
  10. Lines 170-183: Updates status info
    - Sets last sync time and hash
    - Updates status condition to "Ready: True"
    - Updates the resource status
  11. Line 187-188: Requeues based on refresh interval

  Lines 191-239: reconcileDelete function

  Handles deletion of ConfigMapSource resources:

  1. Lines 196-200: Gets target namespace
  2. Lines 202-228: Deletes owned ConfigMap if appropriate
    - Only deletes if in same namespace
    - Only deletes if owned by this ConfigMapSource
  3. Lines 230-235: Removes finalizer to allow deletion
  4. Lines 237-238: Logs success and returns

  Lines 241-255: fetchConfigData function

  Dispatches to appropriate source-specific fetch method:
  - Git repository
  - File
  - ConfigMap
  - Secret

  Lines 257-327: fetchFromGit function

  Fetches configuration from a Git repository:
  1. Validates Git source configuration
  2. Creates a temporary directory
  3. Sets up SSH authentication if needed
  4. Clones the repository
  5. Reads configuration files from specified path

  Lines 329-338: fetchFromFile function

  Reads configuration from a local file or directory

  Lines 340-381: fetchFromConfigMap function

  Copies data from an existing ConfigMap:
  1. Gets source namespace
  2. Fetches source ConfigMap
  3. Filters keys if specified or copies all data

  Lines 383-424: fetchFromSecret function

  Similar to ConfigMap, but for Secret resources:
  1. Gets source namespace
  2. Fetches source Secret
  3. Filters keys if specified or copies all data
  4. Converts binary data to string

  Lines 426-468: readConfigFiles function

  Reads configuration files:
  1. Checks if path is a file or directory
  2. For directory: reads all files, using filenames as keys
  3. For file: reads content, using basename as key

  Lines 470-488: calculateConfigHash function

  Creates a hash for change detection:
  1. Sorts keys for consistent hashing
  2. Hashes each key-value pair
  3. Returns hex-encoded hash

  Lines 490-499: requeueBasedOnRefreshInterval function

  Determines when to requeue for periodic updates:
  1. If interval specified and greater than 0, returns RequeueAfter
  2. Otherwise, no automatic refresh

  Lines 501-528: setStatusCondition function

  Updates status conditions:
  1. Initializes conditions array if needed
  2. Finds existing condition by type
  3. Updates timestamp and generation
  4. Updates existing condition or adds new one

  Lines 530-538: isOwnedBy function

  Checks if a ConfigMap is owned by a specific ConfigMapSource

  Lines 540-546: SetupWithManager function

  Configures the controller with controller-runtime:
  1. Sets it to watch ConfigMapSource resources
  2. Sets it to watch owned ConfigMap resources
