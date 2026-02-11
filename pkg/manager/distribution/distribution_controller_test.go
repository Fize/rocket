/*
Copyright2021.

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

package distribution

import (
	"context"
	"testing"
	"time"

	rocketv1alpha1 "github.com/hex-techs/rocket/api/v1alpha1"
	"github.com/hex-techs/rocket/pkg/utils/constant"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDistributionReconciler_Reconcile(t *testing.T) {
	scheme := runtime.NewScheme()
	rocketv1alpha1.AddToScheme(scheme)

	tests := []struct {
		name         string
		existingObjs []client.Object
		req          ctrl.Request
		wantResult   ctrl.Result
		wantErr      bool
		verify       func(t *testing.T, c client.Client)
	}{
		{
			name: "add finalizer when creating",
			existingObjs: []client.Object{
				&rocketv1alpha1.Distribution{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-dist",
						Namespace: "default",
					},
				},
			},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dist",
					Namespace: "default",
				},
			},
			wantResult: ctrl.Result{},
			wantErr:    false,
			verify: func(t *testing.T, c client.Client) {
				d := &rocketv1alpha1.Distribution{}
				err := c.Get(context.Background(), types.NamespacedName{Name: "test-dist", Namespace: "default"}, d)
				if err != nil {
					t.Errorf("Get distribution error: %v", err)
				}
				found := false
				for _, f := range d.Finalizers {
					if f == constant.DistributionFinalizer {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Finalizer not added")
				}
			},
		},
		{
			name: "remove finalizer when deleting and conditions met",
			existingObjs: []client.Object{
				&rocketv1alpha1.Distribution{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-dist-del",
						Namespace:         "default",
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
						Finalizers:        []string{constant.DistributionFinalizer},
					},
					Status: rocketv1alpha1.DistributionStatus{
						Conditions: map[string]rocketv1alpha1.DistributionCondition{
							"cluster1": {
								Reason: ResourceDeleteReason,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			req: ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-dist-del",
					Namespace: "default",
				},
			},
			wantResult: ctrl.Result{},
			wantErr:    false,
			verify: func(t *testing.T, c client.Client) {
				d := &rocketv1alpha1.Distribution{}
				err := c.Get(context.Background(), types.NamespacedName{Name: "test-dist-del", Namespace: "default"}, d)
				if errors.IsNotFound(err) {
					// Object deleted, which means finalizer was removed and GC collected it.
					return
				}
				if err != nil {
					t.Errorf("Get distribution error: %v", err)
					return
				}
				for _, f := range d.Finalizers {
					if f == constant.DistributionFinalizer {
						t.Errorf("Finalizer not removed")
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingObjs...).Build()
			r := &DistributionReconciler{
				Client: client,
				Scheme: scheme,
			}

			got, err := r.Reconcile(context.Background(), tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantResult {
				t.Errorf("Reconcile() = %v, want %v", got, tt.wantResult)
			}

			if tt.verify != nil {
				tt.verify(t, client)
			}
		})
	}
}
