# Refactoring Plan & Technical Debt

This document outlines identified code duplication and technical debt items that were found during the code review but have not yet been resolved. These items are considered non-blocking for production but should be addressed to improve maintainability.

## 1. Unstructured Workload Boilerplate Duplication (Issue #3)

**Description:**
The pattern for creating an `*unstructured.Unstructured` object and setting its GVK (GroupVersionKind) and metadata is repeated multiple times across controllers.

**Locations:**
- `internal/manager/application/controller.go`: 5 occurrences
- `internal/manager/application/status_controller.go`: 1 occurrence

**Code Pattern:**
```go
u := &unstructured.Unstructured{}
u.SetAPIVersion(app.Spec.Workload.APIVersion)
u.SetKind(app.Spec.Workload.Kind)
u.SetName(app.Name)
u.SetNamespace(app.Namespace)
```

**Suggested Fix:**
Refactor into a factory function, e.g., `NewWorkloadUnstructured(app *appsv1alpha1.Application) *unstructured.Unstructured`.

---

## 2. Inconsistent Workload Status Extraction Logic (Issue #4)

**Description:**
Logic for extracting `replicas`, `readyReplicas`, and `availableReplicas` from unstructured objects is duplicated and slightly inconsistent between controllers.

**Locations:**
- `internal/manager/application/controller.go` (Lines 258-282)
- `internal/manager/application/status_controller.go` (Lines 225-254)

**Inconsistencies:**
- `controller.go` checks `status.replicas` while `status_controller.go` checks `spec.replicas` (which might be intended, but worthy of review).
- `status_controller.go` includes logic to check for "Degraded" conditions, whereas `controller.go` does not.

**Suggested Fix:**
Consolidate into a shared helper function, e.g., `ExtractWorkloadStatus(u *unstructured.Unstructured) appsv1alpha1.ClusterStatus`.

---

## 3. Health Phase Calculation Logic Duplication (Issue #5)

**Description:**
The logic for calculating the overall application health phase based on cluster statuses is copy-pasted.

**Locations:**
- `internal/manager/application/status_controller.go`: Inside `calculatePhase()` function.
- `internal/manager/application/status_controller.go`: Inside `aggregateStatus()` function (inline loop).

**Suggested Fix:**
Standardize `aggregateStatus()` to call `r.calculatePhase()` instead of implementing the loop inline.

---

## 4. "Not Found" Error Check Pattern Duplication (Issue #6)

**Description:**
A loose check for "not found" errors (combining `errors.IsNotFound` and string matching) is repeated.

**Locations:**
- `internal/manager/application/controller.go` (2 occurrences)

**Code Pattern:**
```go
if errors.IsNotFound(err) || (err != nil && strings.Contains(err.Error(), "not found")) {
```

**Suggested Fix:**
Extract into a helper function in a utility package, e.g., `pkg/util/k8sutil/errors.go`: `func IsResourceNotFound(err error) bool`.

---

## 5. SSA Fallback Error Check Duplication (Issue #9)

**Description:**
Hardcoded string check for Server-Side Apply (SSA) support.

**Locations:**
- `internal/manager/application/controller.go` (2 occurrences)

**Code Pattern:**
```go
strings.Contains(err.Error(), "apply patches are not supported")
```

**Suggested Fix:**
Extract into a helper function, e.g., `func IsSSAUnsupported(err error) bool`.
