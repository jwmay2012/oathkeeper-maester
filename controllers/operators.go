// Copyright © 2023 Ory Corp
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/avast/retry-go"
	"github.com/go-logr/logr"
	oathkeeperv1alpha1 "github.com/ory/oathkeeper-maester/api/v1alpha1"
	apiv1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OperatorMode is an interface that provides runtime strategy for operating mode ("controller" or "sidecar").
type OperatorMode interface {
	// CreateOrUpdate ORY Oathkeeper Access Rule list using implementation-specific means.
	// oathkeeperRulesJSON - serialized JSON with an array of objects that conform to Oathkeeper Rule syntax
	// triggeredBy - the recently created/update rule that triggered the operation
	CreateOrUpdate(ctx context.Context, oathkeeperRulesJSON []byte, triggeredBy *oathkeeperv1alpha1.Rule) error
}

// ConfigMapOperator that maintains Oathkeeper rules as an json-formatted entry in a ConfigMap
type ConfigMapOperator struct {
	client.Client
	Log              logr.Logger
	DefaultConfigMap types.NamespacedName
	RulesFileName    string
}

// FilesOperator that maintains Oathkeeper rules as a flat json file in a local filesystem
type FilesOperator struct {
	Log           logr.Logger
	RulesFilePath string
}

func (cmo *ConfigMapOperator) updateOrCreateRulesConfigmap(ctx context.Context, configMap types.NamespacedName, data string) error {

	var oathkeeperRulesConfigmap apiv1.ConfigMap
	var exists = false

	fetchMapFunc := func() error {

		if err := cmo.Get(ctx, configMap, &oathkeeperRulesConfigmap); err != nil {

			if apierrs.IsForbidden(err) {
				return retry.Unrecoverable(err)
			}

			if apierrs.IsNotFound(err) {
				return nil
			}

			return err
		}

		exists = true
		return nil
	}

	createMapFunc := func() error {
		cmo.Log.Info("creating ConfigMap")
		oathkeeperRulesConfigmap = apiv1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMap.Name,
				Namespace: configMap.Namespace,
			},
			Data: map[string]string{cmo.RulesFileName: data},
		}
		return cmo.Create(ctx, &oathkeeperRulesConfigmap)
	}

	updateMapFunc := func() error {
		cmo.Log.Info("updating ConfigMap")
		oathkeeperRulesConfigmap.Data = map[string]string{cmo.RulesFileName: data}
		err := cmo.Update(ctx, &oathkeeperRulesConfigmap)
		return err
	}

	return retryOnError(func() error {
		exists = false

		if err := fetchMapFunc(); err != nil {
			return err
		}

		if exists {
			err := updateMapFunc()
			if err != nil {
				if isObjectHasBeenModified(err) {
					cmo.Log.Error(err, "incorrect object version during ConfigMap update")
				}
			}
			return err
		}

		return createMapFunc()
	})
}

func (cmo *ConfigMapOperator) CreateOrUpdate(ctx context.Context, oathkeeperRulesJSON []byte, triggeredBy *oathkeeperv1alpha1.Rule) error {

	configMapRef := cmo.DefaultConfigMap
	if triggeredBy != nil && triggeredBy.Spec.ConfigMapName != nil && len(*triggeredBy.Spec.ConfigMapName) > 0 {
		configMapRef = types.NamespacedName{
			Name:      *triggeredBy.Spec.ConfigMapName,
			Namespace: triggeredBy.ObjectMeta.Namespace,
		}
	}
	return cmo.updateOrCreateRulesConfigmap(ctx, configMapRef, string(oathkeeperRulesJSON))
}

func (fo *FilesOperator) updateOrCreateRulesFile(ctx context.Context, data string) error {
	mode := os.FileMode(0644)
	if info, err := os.Stat(fo.RulesFilePath); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}

	// Publish a complete snapshot without truncating the watched file.
	f, err := os.CreateTemp(filepath.Dir(fo.RulesFilePath), ".access-rules-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.WriteString(data); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), fo.RulesFilePath); err != nil {
		return err
	}
	fo.Log.Info(fmt.Sprintf("published %d bytes of rules into %s", len(data), fo.RulesFilePath))
	return nil
}

func (fo *FilesOperator) CreateOrUpdate(ctx context.Context, oathkeeperRulesJSON []byte, triggeredBy *oathkeeperv1alpha1.Rule) error {
	if triggeredBy != nil && triggeredBy.Spec.ConfigMapName != nil && len(*triggeredBy.Spec.ConfigMapName) > 0 {
		fo.Log.Info("Ignoring Spec.ConfigMapName value - sidecar mode enabled")
	}

	return fo.updateOrCreateRulesFile(ctx, string(oathkeeperRulesJSON))
}
