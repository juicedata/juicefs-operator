/*
Copyright 2025 Juicedata Inc

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

package builder

import (
	"context"
	"fmt"

	"maps"

	juicefsiov1 "github.com/juicedata/juicefs-operator/api/v1"
	"github.com/juicedata/juicefs-operator/pkg/common"
	"github.com/juicedata/juicefs-operator/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func genJuiceFSSecretData(juicefs *juicefsiov1.SyncSinkJuiceFS, suffix string) map[string]string {
	if juicefs == nil {
		return nil
	}
	data := make(map[string]string)
	if juicefs.Token.Value != "" {
		data[fmt.Sprintf("%s_TOKEN", suffix)] = juicefs.Token.Value
	}
	if juicefs.AccessKey.Value != "" {
		data[fmt.Sprintf("%s_ACCESS_KEY", suffix)] = juicefs.AccessKey.Value
	}
	if juicefs.SecretKey.Value != "" {
		data[fmt.Sprintf("%s_SECRET_KEY", suffix)] = juicefs.SecretKey.Value
	}
	return data
}

func genExternalSecretData(external *juicefsiov1.SyncSinkExternal, suffix string) map[string]string {
	if external == nil {
		return nil
	}
	data := make(map[string]string)
	if external.AccessKey.Value != "" {
		data[fmt.Sprintf("%s_ACCESS_KEY", suffix)] = external.AccessKey.Value
	}
	if external.SecretKey.Value != "" {
		data[fmt.Sprintf("%s_SECRET_KEY", suffix)] = external.SecretKey.Value
	}
	return data
}

func genJuiceFSCESecretData(juicefsCE *juicefsiov1.SyncSinkJuiceFSCE, suffix string) map[string]string {
	if juicefsCE == nil {
		return nil
	}
	data := make(map[string]string)
	if juicefsCE.MetaPassWord.Value != "" {
		data[fmt.Sprintf("%s_META_PASSWORD", suffix)] = juicefsCE.MetaPassWord.Value
	}
	return data
}

func NewSyncSecret(ctx context.Context, sync *juicefsiov1.Sync) (*corev1.Secret, error) {
	secretName := common.GenSyncSecretName(sync.Name)
	data := make(map[string]string)

	if utils.IsDistributed(sync) {
		id_rsa, id_rsa_pub, err := utils.GenerateSSHKeyPair()
		if err != nil {
			return nil, err
		}
		data["id_rsa"] = id_rsa
		data["id_rsa.pub"] = id_rsa_pub
	}
	maps.Copy(data, genJuiceFSSecretData(sync.Spec.From.JuiceFS, "JUICEFS_FROM"))
	maps.Copy(data, genJuiceFSSecretData(sync.Spec.To.JuiceFS, "JUICEFS_TO"))
	maps.Copy(data, genExternalSecretData(sync.Spec.From.External, "EXTERNAL_FROM"))
	maps.Copy(data, genExternalSecretData(sync.Spec.To.External, "EXTERNAL_TO"))
	maps.Copy(data, genJuiceFSCESecretData(sync.Spec.To.JuiceFSCE, "JUICEFS_CE_TO"))
	maps.Copy(data, genJuiceFSCESecretData(sync.Spec.From.JuiceFSCE, "JUICEFS_CE_FROM"))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: sync.Namespace,
		},
		StringData: data,
	}

	secret.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: common.GroupVersion,
			Kind:       common.KindSync,
			Name:       sync.Name,
			UID:        sync.UID,
			Controller: utils.ToPtr(true),
		},
	})
	return secret, nil
}
