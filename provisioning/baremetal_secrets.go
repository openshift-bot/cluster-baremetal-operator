package provisioning

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
)

const (
	baremetalSecretName = "metal3-mariadb-password" // #nosec
	baremetalSecretKey  = "password"
	ironicUsernameKey   = "username"
	ironicPasswordKey   = "password"
	ironicHtpasswdKey   = "htpasswd"
	ironicConfigKey     = "auth-config"
	ironicSecretName    = "metal3-ironic-password"
	ironicrpcSecretName = "metal3-ironic-rpc-password" // #nosec
	ironicrpcUsername   = "rpc-user"
	ironicUsername      = "ironic-user"
	inspectorSecretName = "metal3-ironic-inspector-password"
	inspectorUsername   = "inspector-user"
	tlsSecretName       = "metal3-ironic-tls" // #nosec
	tlsCertificateKey   = "tls.crt"
	tlsPrivateKeyKey    = "tls.key"
)

// CreateMariadbPasswordSecret creates a Secret for Mariadb password
func createMariadbPasswordSecret(client coreclientv1.SecretsGetter, targetNamespace string, baremetalConfig *metal3iov1alpha1.Provisioning, scheme *runtime.Scheme) error {
	existing, err := client.Secrets(targetNamespace).Get(context.Background(), baremetalSecretName, metav1.GetOptions{})
	if err == nil && len(existing.ObjectMeta.OwnerReferences) == 0 {
		err = controllerutil.SetControllerReference(baremetalConfig, existing, scheme)
		if err != nil {
			return err
		}
		_, err = client.Secrets(targetNamespace).Update(context.Background(), existing, metav1.UpdateOptions{})
		return err
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Secret does not already exist. So, create one.
	password, err := generateRandomPassword()
	if err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      baremetalSecretName,
			Namespace: targetNamespace,
		},
		StringData: map[string]string{
			baremetalSecretKey: password,
		},
	}

	err = controllerutil.SetControllerReference(baremetalConfig, secret, scheme)
	if err != nil {
		return err
	}

	_, err = client.Secrets(targetNamespace).Create(context.Background(), secret, metav1.CreateOptions{})

	return err
}

func createIronicSecret(client coreclientv1.SecretsGetter, targetNamespace string, name string, username string, configSection string, baremetalConfig *metal3iov1alpha1.Provisioning, scheme *runtime.Scheme) error {
	existing, err := client.Secrets(targetNamespace).Get(context.Background(), name, metav1.GetOptions{})
	if err == nil && len(existing.ObjectMeta.OwnerReferences) == 0 {
		err = controllerutil.SetControllerReference(baremetalConfig, existing, scheme)
		if err != nil {
			return err
		}
		_, err = client.Secrets(targetNamespace).Update(context.Background(), existing, metav1.UpdateOptions{})
		return err
	}

	if !apierrors.IsNotFound(err) {
		return err
	}

	// Secret does not already exist. So, create one.
	password, err := generateRandomPassword()
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 5) // Use same cost as htpasswd default
	if err != nil {
		return err
	}
	// Change hash version from $2a$ to $2y$, as generated by htpasswd.
	// These are equivalent for our purposes.
	// Some background information about this : https://en.wikipedia.org/wiki/Bcrypt#Versioning_history
	// There was a bug 9 years ago in PHP's implementation of 2a, so they decided to call the fixed version 2y.
	// httpd decided to adopt this (if it sees 2a it uses elaborate heuristic workarounds to mitigate against the bug,
	// but 2y is assumed to not need them), but everyone else (including go) was just decided to not implement the bug in 2a.
	// The bug only affects passwords containing characters with the high bit set, i.e. not ASCII passwords generated here.

	// Anyway, Ironic implemented their own basic auth verification and originally hard-coded 2y because that's what
	// htpasswd produces (see https://review.opendev.org/738718). It is better to keep this as one day we may move the auth
	// to httpd and this would prevent triggering the workarounds.
	hash[2] = 'y'

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: targetNamespace,
		},
		StringData: map[string]string{
			ironicUsernameKey: username,
			ironicPasswordKey: password,
			ironicHtpasswdKey: fmt.Sprintf("%s:%s", username, hash),
			ironicConfigKey: fmt.Sprintf(`[%s]
auth_type = http_basic
username = %s
password = %s
`,
				configSection, username, password),
		},
	}

	err = controllerutil.SetControllerReference(baremetalConfig, secret, scheme)
	if err != nil {
		return err
	}

	_, err = client.Secrets(targetNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
	return err
}

func CreateAllSecrets(client coreclientv1.SecretsGetter, targetNamespace string, baremetalConfig *metal3iov1alpha1.Provisioning, scheme *runtime.Scheme) error {
	// Create a Secret for the Mariadb Password
	if err := createMariadbPasswordSecret(client, targetNamespace, baremetalConfig, scheme); err != nil {
		return errors.Wrap(err, "failed to create Mariadb password")
	}
	// Create a Secret for the Ironic Password
	if err := createIronicSecret(client, targetNamespace, ironicSecretName, ironicUsername, "ironic", baremetalConfig, scheme); err != nil {
		return errors.Wrap(err, "failed to create Ironic password")
	}
	// Create a Secret for the Ironic RPC Password
	if err := createIronicSecret(client, targetNamespace, ironicrpcSecretName, ironicrpcUsername, "json_rpc", baremetalConfig, scheme); err != nil {
		return errors.Wrap(err, "failed to create Ironic rpc password")
	}
	// Create a Secret for the Ironic Inspector Password
	if err := createIronicSecret(client, targetNamespace, inspectorSecretName, inspectorUsername, "inspector", baremetalConfig, scheme); err != nil {
		return errors.Wrap(err, "failed to create Inspector password")
	}
	// Generate/update TLS certificate
	if err := CreateOrUpdateTlsSecret(client, targetNamespace, baremetalConfig, scheme); err != nil {
		return errors.Wrap(err, "failed to create TLS certificate")
	}
	return nil
}

func DeleteAllSecrets(info *ProvisioningInfo) error {
	var secretErrors []error
	for _, sn := range []string{baremetalSecretName, ironicSecretName, inspectorSecretName, ironicrpcSecretName} {
		if err := client.IgnoreNotFound(info.Client.CoreV1().Secrets(info.Namespace).Delete(context.Background(), sn, metav1.DeleteOptions{})); err != nil {
			secretErrors = append(secretErrors, err)
		}
	}
	return utilerrors.NewAggregate(secretErrors)
}

func updateTlsSecret(client coreclientv1.SecretsGetter, targetNamespace string, baremetalConfig *metal3iov1alpha1.Provisioning, scheme *runtime.Scheme, secret *corev1.Secret) error {
	changed := false

	if len(secret.ObjectMeta.OwnerReferences) == 0 {
		err := controllerutil.SetControllerReference(baremetalConfig, secret, scheme)
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("failed to set controller reference of Secret %s", tlsSecretName))
		}
		changed = true
	}

	existingCert := secret.StringData[tlsCertificateKey]
	if existingCert != "" {
		expired, err := IsTlsCertificateExpired(existingCert)
		if err != nil {
			return errors.Wrap(err, "failed to determine expiration date of TLS certificate")
		}

		if !expired {
			return nil
		}
	}

	cert, err := generateTlsCertificate(baremetalConfig.Spec.ProvisioningIP)
	if err != nil {
		return errors.Wrap(err, "failed to generate new TLS certificate")
	}
	secret.StringData = map[string]string{tlsCertificateKey: cert.certificate, tlsPrivateKeyKey: cert.privateKey}
	changed = true

	if changed {
		_, err = client.Secrets(targetNamespace).Update(context.Background(), secret, metav1.UpdateOptions{})
		return err
	}

	return nil

}

// CreateOrUpdateTlsSecret creates a Secret for the Ironic and Inspector TLS.
// It updates the secret if the existing certificate is close to expiration.
func CreateOrUpdateTlsSecret(client coreclientv1.SecretsGetter, targetNamespace string, baremetalConfig *metal3iov1alpha1.Provisioning, scheme *runtime.Scheme) error {
	existing, err := client.Secrets(targetNamespace).Get(context.Background(), tlsSecretName, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if err == nil {
		return updateTlsSecret(client, targetNamespace, baremetalConfig, scheme, existing)
	}

	// Secret does not already exist. So, create one.
	cert, err := generateTlsCertificate(baremetalConfig.Spec.ProvisioningIP)
	if err != nil {
		return err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      tlsSecretName,
			Namespace: targetNamespace,
		},
		StringData: map[string]string{
			tlsCertificateKey: cert.certificate,
			tlsPrivateKeyKey:  cert.privateKey,
		},
	}

	err = controllerutil.SetControllerReference(baremetalConfig, secret, scheme)
	if err != nil {
		return err
	}

	_, err = client.Secrets(targetNamespace).Create(context.Background(), secret, metav1.CreateOptions{})

	return err
}
