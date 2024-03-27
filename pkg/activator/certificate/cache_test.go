/*
Copyright 2023 The Knative Authors

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

package certificate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"knative.dev/networking/pkg/apis/networking"
	"knative.dev/pkg/reconciler"

	"knative.dev/networking/pkg/certificates"
	netcfg "knative.dev/networking/pkg/config"
	fakekubeclient "knative.dev/pkg/client/injection/kube/client/fake"
	fakeconfigmapinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/configmap/fake"
	fakesecretinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/secret/fake"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	rtesting "knative.dev/pkg/reconciler/testing"
	"knative.dev/pkg/system"
)

func TestReconcile(t *testing.T) {

	tests := []struct {
		name         string
		secret       *corev1.Secret
		configMap    *corev1.ConfigMap
		expectedPool *x509.CertPool
	}{{
		name:         "Valid CA only in secret",
		secret:       secret,
		configMap:    nil,
		expectedPool: getPoolWithCerts(secret.Data[certificates.CaCertName]),
	},
		{
			name: "Valid CA only in configmap",
			secret: func() *corev1.Secret {
				s := secret.DeepCopy()
				delete(s.Data, certificates.CaCertName)
				return s
			}(),
			configMap:    validConfigmap,
			expectedPool: getPoolWithCerts(configmapCA),
		},
		{
			name:         "Valid CA in both configmap and secret",
			secret:       secret,
			configMap:    validConfigmap,
			expectedPool: getPoolWithCerts(secretCA, configmapCA),
		},
		{
			name:         "Invalid CA in configmap",
			secret:       secret,
			configMap:    invalidConfigmap,
			expectedPool: getPoolWithCerts(secretCA),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Setup
			ctx, cancel, informers := rtesting.SetupFakeContextWithCancel(t)
			cr := fakeCertCache(ctx)
			waitInformers, err := rtesting.RunAndSyncInformers(ctx, informers...)
			if err != nil {
				cancel()
				t.Fatal("failed to start informers:", err)
			}
			t.Cleanup(func() {
				cancel()
				waitInformers()
			})

			// Data preparation
			if test.secret != nil {
				fakekubeclient.Get(ctx).CoreV1().Secrets(system.Namespace()).Create(ctx, test.secret, metav1.CreateOptions{})
				fakesecretinformer.Get(ctx).Informer().GetIndexer().Add(test.secret)
			}
			if test.configMap != nil {
				fakekubeclient.Get(ctx).CoreV1().ConfigMaps(system.Namespace()).Create(ctx, test.configMap, metav1.CreateOptions{})
				fakeconfigmapinformer.Get(ctx).Informer().GetIndexer().Add(test.configMap)
			}

			// Verify
			if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 2*time.Second, true, func(context.Context) (bool, error) {
				cr.certificatesMux.RLock()
				defer cr.certificatesMux.RUnlock()
				cert, _ := cr.GetCertificate(nil)
				return cert != nil && cr.TLSConf.RootCAs.Equal(test.expectedPool), nil
			}); err != nil {
				t.Fatalf("Did not meet the expected state: %s, err: %v", test.name, err)
			}
		})
	}

	// Additional secret tests
	ctx, cancel, informers := rtesting.SetupFakeContextWithCancel(t)
	cr := fakeCertCache(ctx)
	waitInformers, err := rtesting.RunAndSyncInformers(ctx, informers...)
	if err != nil {
		cancel()
		t.Fatal("failed to start informers:", err)
	}
	t.Cleanup(func() {
		cancel()
		waitInformers()
	})
	fakekubeclient.Get(ctx).CoreV1().Secrets(system.Namespace()).Create(ctx, secret, metav1.CreateOptions{})
	fakesecretinformer.Get(ctx).Informer().GetIndexer().Add(secret)

	// Update cert and key but keep using old CA, then the error is expected.
	newCertSecret := secret.DeepCopy()
	newCertSecret.Data[certificates.CertName] = newTLSCrt
	newCertSecret.Data[certificates.PrivateKeyName] = newTLSKey
	newCert, _ := tls.X509KeyPair(newTLSCrt, newTLSKey)

	fakekubeclient.Get(ctx).CoreV1().Secrets(system.Namespace()).Update(ctx, newCertSecret, metav1.UpdateOptions{})
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		cr.certificatesMux.RLock()
		defer cr.certificatesMux.RUnlock()
		cert, err := cr.GetCertificate(nil)
		return err == nil && cert != nil && reflect.DeepEqual(newCert.Certificate, cert.Certificate), nil
	}); err != nil {
		t.Fatalf("timeout to update the cert: %v", err)
	}

	// Update CA, now the error is gone.
	newCASecret := newCertSecret.DeepCopy()
	newCASecret.Data[certificates.CaCertName] = newCA
	expectedPool := getPoolWithCerts(newCASecret.Data[certificates.CaCertName])
	fakekubeclient.Get(ctx).CoreV1().Secrets(system.Namespace()).Update(ctx, newCASecret, metav1.UpdateOptions{})
	if err := wait.PollUntilContextTimeout(ctx, 10*time.Millisecond, 10*time.Second, true, func(context.Context) (bool, error) {
		cr.certificatesMux.RLock()
		defer cr.certificatesMux.RUnlock()
		return cr.TLSConf.RootCAs.Equal(expectedPool), nil
	}); err != nil {
		t.Fatalf("timeout to update the cert: %v", err)
	}
}

func fakeCertCache(ctx context.Context) *CertCache {
	secretInformer := fakesecretinformer.Get(ctx)
	configmapInformer := fakeconfigmapinformer.Get(ctx)

	cr := &CertCache{
		secretInformer:    secretInformer,
		configmapInformer: configmapInformer,
		certificate:       nil,
		TLSConf:           tls.Config{},
		logger:            logging.FromContext(ctx),
	}

	secretInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controller.FilterWithNameAndNamespace(system.Namespace(), netcfg.ServingRoutingCertName),
		Handler: cache.ResourceEventHandlerFuncs{
			UpdateFunc: cr.handleCertificateUpdate,
			AddFunc:    cr.handleCertificateAdd,
		},
	})

	configmapInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: reconciler.ChainFilterFuncs(
			reconciler.LabelExistsFilterFunc(networking.TrustBundleLabelKey),
		),
		Handler: controller.HandleAll(func(obj interface{}) {
			cr.updateTrustPool()
		}),
	})

	return cr
}

func getPoolWithCerts(certs ...[]byte) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, c := range certs {
		pool.AppendCertsFromPEM(c)
	}
	return pool
}

var secretCA = []byte(
	`-----BEGIN CERTIFICATE-----
MIIDRzCCAi+gAwIBAgIQQo5HxzWURsnoOnFP1U2vKjANBgkqhkiG9w0BAQsFADAu
MRQwEgYDVQQKEwtrbmF0aXZlLmRldjEWMBQGA1UEAxMNY29udHJvbC1wbGFuZTAe
Fw0yMzA0MDUwNjM2NDFaFw0zMzA0MDIwNjM2NDFaMC4xFDASBgNVBAoTC2tuYXRp
dmUuZGV2MRYwFAYDVQQDEw1jb250cm9sLXBsYW5lMIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEAr4Qluf/wxjPkVCVxAdUTh82RbnMBMWqunDuEOml535NX
SIzDrqLajJ1NIxG+PLrWuBkXvY+nIuo7dnynh8JP/ILFlv8d0NW4St+aY6ku1g6p
OsVRtETSlRNhsfmZqe6zyoEaNwJmZMxr3zMYPD0xdXdDXaORbBAQfXHN09Zbx4hS
JBSXItKQQvvKYzPWBWB32UHyD7qDJb9sqIOSZFeREz74RleWWOOg4JsZHio8/Y1I
vu4yAMVQagKNl8bdsVg0YTdOBlxBUgSuz1HxvMjUijc+GSSZGoV+qABUMHrMVKS4
L5LQhAuoP8XhTu9ZZDTDGmDv0RShjNVZ2X5Gt8WSxwIDAQABo2EwXzAOBgNVHQ8B
Af8EBAMCAoQwHQYDVR0lBBYwFAYIKwYBBQUHAwEGCCsGAQUFBwMCMA8GA1UdEwEB
/wQFMAMBAf8wHQYDVR0OBBYEFKIE61RrrGIRNTOj7JbtfS1xAnJtMA0GCSqGSIb3
DQEBCwUAA4IBAQAKL4L9o/WKznCqP9IMXnoN86PCKbJGfV6Q8PuwQ/FhMGAWhfGj
REyUap3Y88c3+7gCr8puXFOjWicrEEdvWgcFIkO+Rs8br6KD4GGdDn8lGIYIxZ1A
ehGaU9eIWWS9swIR+sQxO2yPjJ/mNLI+MQH9E2Q0rY4b6669oCMtenXJ3kDl2x/V
stXnfHpI3C14v4t00tZ9JHVNIeyGapXmKpq8rys5bnDWoUv62I+W/s4Kx3jUx2pO
7aXrDONR1fhKKHu7k1lJHfa6t34YeYTfZZk0viLF50pvPbrbAFHHZ00EyQGA1Zur
yycz8WDCfsgD/kiJ3GiITxBH8oHotFZdjFnF
-----END CERTIFICATE-----`)

var configmapCA = []byte(
	`-----BEGIN CERTIFICATE-----
MIIDDTCCAfWgAwIBAgIQMQuip05h7NLQq2TB+j9ZmTANBgkqhkiG9w0BAQsFADAW
MRQwEgYDVQQDEwtrbmF0aXZlLmRldjAeFw0yMzExMjIwOTAwNDhaFw0yNDAyMjAw
OTAwNDhaMBYxFDASBgNVBAMTC2tuYXRpdmUuZGV2MIIBIjANBgkqhkiG9w0BAQEF
AAOCAQ8AMIIBCgKCAQEA3clC3CV7sy0TpUKNuTku6QmP9z8JUCbLCPCLACCUc1zG
FEokqOva6TakgvAntXLkB3TEsbdCJlNm6qFbbko6DBfX6rEggqZs40x3/T+KH66u
4PvMT3fzEtaMJDK/KQOBIvVHrKmPkvccUYK/qWY7rgBjVjjLVSJrCn4dKaEZ2JNr
Fd0KNnaaW/dP9/FvviLqVJvHnTMHH5qyRRr1kUGTrc8njRKwpHcnUdauiDoWRKxo
Zlyy+MhQfdbbyapX984WsDjCvrDXzkdGgbRNAf+erl6yUm6pHpQhyFFo/zndx6Uq
QXA7jYvM2M3qCnXmaFowidoLDsDyhwoxD7WT8zur/QIDAQABo1cwVTAOBgNVHQ8B
Af8EBAMCAgQwEwYDVR0lBAwwCgYIKwYBBQUHAwEwDwYDVR0TAQH/BAUwAwEB/zAd
BgNVHQ4EFgQU7p4VuECNOcnrP9ulOjc4J37Q2VUwDQYJKoZIhvcNAQELBQADggEB
AAv26Vnk+ptQrppouF7yHV8fZbfnehpm07HIZkmnXO2vAP+MZJDNrHjy8JAVzXjt
+OlzqAL0cRQLsUptB0btoJuw23eq8RXgJo05OLOPQ2iGNbAATQh2kLwBWd/CMg+V
KJ4EIEpF4dmwOohsNR6xa/JoArIYH0D7gh2CwjrdGZr/tq1eMSL+uZcuX5OiE44A
2oXF9/jsqerOcH7QUMejSnB8N7X0LmUvH4jAesQgr7jo1JTOBs7GF6wb+U76NzFa
8ms2iAWhoplQ+EHR52wffWb0k6trXspq4O6v/J+nq9Ky3vC36so+G1ZFkMhCdTVJ
ZmrBsSMWeT2l07qeei2UFRU=
-----END CERTIFICATE-----`)

var tlsCrt = []byte(
	`-----BEGIN CERTIFICATE-----
MIIDkzCCAnugAwIBAgIQe0pM5sKLGeu6+P0xpW+TJTANBgkqhkiG9w0BAQsFADAu
MRQwEgYDVQQKEwtrbmF0aXZlLmRldjEWMBQGA1UEAxMNY29udHJvbC1wbGFuZTAe
Fw0yMzA0MDUwNjM2NDFaFw0yMzA1MDUwNjM2NDFaMD0xFDASBgNVBAoTC2tuYXRp
dmUuZGV2MSUwIwYDVQQDExxjb250cm9sLXByb3RvY29sLWNlcnRpZmljYXRlMIIB
IjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAxAXirxhulBmW+vFQzpvVZLhZ
X9vePIo7M3UiCF4uBXNFPxlVQ8jw8ftMIijybRib0Q3ZiJWDGxfvnNIkuytPrDui
M1Va9652aWVL+px8xvgk3lpJrjlDaYwA5AOKUVH8L50G0FiZdnSU5hC8mfTHlzlX
mOlxGDl6dtrC38gHspOqlZx+yBDfTs3//axgGN/6BWrQegNmCxnfqH1sbzsjLHx+
X21T1bg+ieZ9xKOvckT+trS0/8ujf0APBjMpBrZJPJw0iNtfUd8c528+dOe9T7z7
WzOsAuV5AMa5qj5ZCAWlCQbSIt9qAelz2knGR/Esmy+tEDeLD4+ZcnAiYylTTQID
AQABo4GdMIGaMA4GA1UdDwEB/wQEAwIHgDAdBgNVHSUEFjAUBggrBgEFBQcDAQYI
KwYBBQUHAwIwDAYDVR0TAQH/BAIwADAfBgNVHSMEGDAWgBSiBOtUa6xiETUzo+yW
7X0tcQJybTA6BgNVHREEMzAxghdrbmF0aXZlLWtuYXRpdmUtc2VydmluZ4IWZGF0
YS1wbGFuZS5rbmF0aXZlLmRldjANBgkqhkiG9w0BAQsFAAOCAQEAnq60YB9phe+M
sGOjJJqBKfgFKUpFhcXXHwFJ/pqUPosmCsSw4U9QM8GVE/sRNfbJBLhkN1RyS5j7
VibomK3zrkJw1/WWR3cYTa2UBdYHRBXi4Y3lr9vsPRQY+PUAANwSBqjWmyxcsb/i
xtN0eLeHQLAllzetaWOjy3g58rXx+ZbTtHPqhuQC+CUxS+UPalQN67M/tT/F+ebT
qY0uC3DYSLcbFvddgt6fD0e/eRkyeSehORXphdfjv7QdtI8/TdyArSs16HERTjR6
+UVKygimsrsvP7R7Ku52fNo9b12CGRfFFNvNtVURovJGPz12SGTOpDVmaui/dXuW
FhVeoeczrw==
-----END CERTIFICATE-----`)

var tlsKey = []byte(
	`-----BEGIN RSA PRIVATE KEY-----
MIIEpQIBAAKCAQEAxAXirxhulBmW+vFQzpvVZLhZX9vePIo7M3UiCF4uBXNFPxlV
Q8jw8ftMIijybRib0Q3ZiJWDGxfvnNIkuytPrDuiM1Va9652aWVL+px8xvgk3lpJ
rjlDaYwA5AOKUVH8L50G0FiZdnSU5hC8mfTHlzlXmOlxGDl6dtrC38gHspOqlZx+
yBDfTs3//axgGN/6BWrQegNmCxnfqH1sbzsjLHx+X21T1bg+ieZ9xKOvckT+trS0
/8ujf0APBjMpBrZJPJw0iNtfUd8c528+dOe9T7z7WzOsAuV5AMa5qj5ZCAWlCQbS
It9qAelz2knGR/Esmy+tEDeLD4+ZcnAiYylTTQIDAQABAoIBAQCWnDcJdWow3GCG
urbtqAoTcxkob9SXC1ZlORBHAaW2hlSkIKDEjjWilwRuEqwBarD9tPh42vd6768o
/MVAEg0LNl5vtptIRoGwhSYVjfrJHYumVBTcih7jj7B3gMjbpnRvWOUNW6W9v+FP
y3g9ijd4V5SYZnSAulj/zSGBsz1G1JkrviRYhdG79sx3GCFTC1MoSUl5BHeztUd/
rCCzzf1Xatp+YSO+yBg1leIZxr6poA8g5fXl4yfQrOlEOPFU5n1v7s2OGjoU0ut2
JcTGQn/7eaSH7Bon7iYyWYdmeWTc74TG47mQjU/S0R/qMODoGybWFbBk+gFkde6y
/r81AgLBAoGBAPIc5c7iuoJkE4xg/THqnXAxrIx3gFLhA0nGlWZyTF463QwV7zPD
FJ+3LfTM0YnhqrUCLWEEhUAgfqnn3j4LFI9OX7+LtSTXKtE4aGoJ2w42Bgu0MAH1
MNkjb4qEIXa+JnYOYx/I8I5j6RKCShqw++KXBfaNsBmtzR5bKUghQbH5AoGBAM9E
ODy9YFDuD4umV0QBZTXUxzHEUIlD1CGjDLKPUWncn/nCZwCqCqhxldqBzoEdoRtY
KCleQVN1Q4Pr+olmvhKvXbq00LG2180GiVd2nUBkKqscr3ugUQnQEAJmQj6cx6LH
y/190lhJWRUOgBOVRR0G1Z785ifyNbq1G4FcGgD1AoGBANvWF2iiAD3zBrj5PA29
/VRpFka5H0Ch5W1wrilGcUdCZYHazMaQRMK8/jKAY2ayDGGs521nQGK43qoByo9F
WlbBEDmJbmJUKSGt+UkHR+sAbL7lzo2Ih+Exxs7cKNJ718psR98NgjeYSoIu4YCY
4S2eeaCkiJjYch41IifHYrJpAoGBAKRCKldotcYthEBmSU5p1K3+vQZh0HmYOauW
rl9sWVcOM/IZ8MuD9wJbUiljKicFNkKXcOyn+BmOGz2XbGwr8oKYXC21Upckko23
mmyoYiM/vtjw2Nmeydp++9EK/YDlewk0UiPI7URujJy1aycZ6zX/zpg7UKNjvtUC
5pN0TF9pAoGAMwtLAcGmOYYGQ529BCXq0jx2TA/xLnTq/G4o1WWXZmJ51K0Vp9J1
fHEYvDuPraJgUw+i+e1Y80tbcqFOCvWwZbnwIgSnZ6wFEQe8GcqIG4uhg/U27ZBL
lM4Cod6LJIrZwFSOuTVNWJ2nC9FI1ut3JJdsVLwR3mnBdW3j8WMEHyQ=
-----END RSA PRIVATE KEY-----`)

var newTLSCrt = []byte(
	`-----BEGIN CERTIFICATE-----
MIIC8jCCAdqgAwIBAgIBADANBgkqhkiG9w0BAQsFADApMRUwEwYDVQQKDAxFeGFt
cGxlIEluYy4xEDAOBgNVBAMMB0V4YW1wbGUwHhcNMjMwNDA2MTIwODAwWhcNMjQw
NDA1MTIwODAwWjApMRUwEwYDVQQKDAxFeGFtcGxlIEluYy4xEDAOBgNVBAMMB0V4
YW1wbGUwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDgmP14WwEjgPJY
Qns57EIYoiZqRBWgNcVGqJL+9sU00ji12RoP+8+Dl5+UivN7LoZERgT3aLJNcGcC
dLqNoLCMOFivAVSolXTJy12LqKk9RrycijogMroTZ/uLWewvvnqeh3lzbOE0PtbE
R3zglxBbE9LSFc4vc9j0ACdv9+pzPhvWRjkFi8ytjKZ2HjyXvDFv9Sf2261WnMe4
+QBDMlwEqqZrZfuFllvIr7MuDie+hgLfYptRKQ3ZcwPTfRNQ3Z8qzjiYxiIGLwyK
2bJghBLwV2PXU4MRdGWRBrCSyEiMyek58E+jSSfR2ZReT3U9qQsd8PVPQjM6f7ps
TdOrqTm5AgMBAAGjJTAjMCEGA1UdEQQaMBiCFmRhdGEtcGxhbmUua25hdGl2ZS5k
ZXYwDQYJKoZIhvcNAQELBQADggEBAEvscH4v9C3FV0YmVu++JjI0FgV+60NS1a5C
68gdWlBf/ruJGjdRuGvR5TLPMjef6xGpxPw4kEb+DUgmYIpnOEFvi8G4lI2Vmul4
8+URRoZrwXEzcOSis7HJb2n8QIfA1qPgQYP4g0V2TIymijtR7B7pZoafRtPruXEP
3uw/XB3I+Z4MyP7BsCJm4i8BWQOKjzWuo6JIl8YxWZ3sEn9ET58hf14SwN95yj+u
gvvT4Jl/4HvdyeFVm8yrp3PzGCtNM1OClZqWS181AXzF4LEw4nIE9CqCXyPeP+YF
5B9SL9ra/ntjD3L4yj1QSVITNjGadnuSghYicY8s08ok2lHboEc=
-----END CERTIFICATE-----`)

var newTLSKey = []byte(
	`-----BEGIN PRIVATE KEY-----
MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQDgmP14WwEjgPJY
Qns57EIYoiZqRBWgNcVGqJL+9sU00ji12RoP+8+Dl5+UivN7LoZERgT3aLJNcGcC
dLqNoLCMOFivAVSolXTJy12LqKk9RrycijogMroTZ/uLWewvvnqeh3lzbOE0PtbE
R3zglxBbE9LSFc4vc9j0ACdv9+pzPhvWRjkFi8ytjKZ2HjyXvDFv9Sf2261WnMe4
+QBDMlwEqqZrZfuFllvIr7MuDie+hgLfYptRKQ3ZcwPTfRNQ3Z8qzjiYxiIGLwyK
2bJghBLwV2PXU4MRdGWRBrCSyEiMyek58E+jSSfR2ZReT3U9qQsd8PVPQjM6f7ps
TdOrqTm5AgMBAAECggEALoTBixoeREJC77DlYPvkPMHo/v2XFRXOBHKJ77Eg623X
PSL4WPMo6fKPpO6au5rJSH7QLIZM1+k+DK4srYTozEInbCf0Zu59wAYVHAYU95Id
IrcmjuCy1a4l1ZkMaF8leoxIxXV5t56EUScVYFcplhOnCMhnakCuYOtfP7uznaaO
KCCgwn0wh1nfFN8wrbFjQiQxFbZ6gZvnLi720c8i+HwVMG16W41FCaI59daN0xNx
q1qVLjEH1cu0xiqPUMxE4OhLXOPhsVTimcoG08ZvnTJxQFhIoEFjYesoiE7OlFd1
7mKWT9SfWB87wYjzh71OdsZRaHc5dGYXzwvQW6Lq2QKBgQD5saqJ56y5sZVMMttc
Sf+nxQBkG2ul+xCOcw77BnAr7vHv0IRAEiXYC20me2hpHoWRpF/vfzU+dCFeF6xx
1RuCzbjcjZ9FG3hQbnWrI3orDXPy+mLufWT6MD04MMfTlyJk0NdSJvC3KNEOQbou
E4N5DW97CYqBELQ3rY1UPhCcEwKBgQDmRRHNwuTJzIIJrjpI4dvAVO3W3pOYAkvl
zIf4LAMWew2Wa8IIh40E6RDX6J0mqVUIR94vyO77SQnIGSUOWmJ2CaBjJm/gU4/s
TaPgDUzUxQOIC6UWBTesMVN55tLMyS7F5Ngs7bYwEi72RiLktF8p9UH+KVc/4Zvn
vE6sLGS0gwKBgQDtIe30SjGfqSdA1ou9eglyK4XTjLcPSwDOSDdR7ytYjfT26/Ct
aI7IPxHKGiluq63uQ01ZBlZqmZ+W3KTI9rrJ3tZRn65C03PP7xeREIBVotEbUO/j
zvK3KFj7pFgiesYPOMdFHfY9/GWORJ2sZJvXuwrErqr7KAH/XrN57feYQQKBgCk4
rBs9jF9jsNOy0NRDOmePzJPufFV188hLePvART09Ag2vdKi6O1BpuI4uIhPNtF8r
HmdHfSCWzp13gt6y53Vh+8hEFTr/OoB+1ZtCRkLAkgVEsGTkwjadDeiAnbPzP+BF
Oz2vwDGSz71eiNiQQYjtUscA95GD/bjaSOshd1WpAoGAeYPYu4U7Hyo5KnhKTuij
s0Sut3ENoDLSbU1sj4nJBxYInyy9pXjYJfeHIkUxBMxce8EFq00q5vQ2VfNYQxgz
LKgjy1VhWvqWQn7tjz8QcQsJroMehCnWeo+IsjzOMCxfKCWgSb0EO877+D2abJP3
uWm3kJYH6ksoReM+82rLRGc=
-----END PRIVATE KEY-----`)

var newCA = []byte(
	`-----BEGIN CERTIFICATE-----
MIIDMzCCAhugAwIBAgIUWdLsI+5SEcIiUOkswKyDSfCsHRgwDQYJKoZIhvcNAQEL
BQAwKTEVMBMGA1UECgwMRXhhbXBsZSBJbmMuMRAwDgYDVQQDDAdFeGFtcGxlMB4X
DTIzMDQwNjEyMDgwMFoXDTI0MDQwNTEyMDgwMFowKTEVMBMGA1UECgwMRXhhbXBs
ZSBJbmMuMRAwDgYDVQQDDAdFeGFtcGxlMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEAyaMrfzPLU7j7bRukm7JKNwewIIcm/AtukVR50S+pHmAos4a+815F
Jf+ThZmQqYbnjg1NPCvw3qFzHKq+HD/knBWZDAlet55ScHsepMeW4u6rkLzIFPZl
e0BmhLGjn1QqPWbNuMmymDm4l7y/lETL+KHUueNlQwXTSiCooMq/fiqv76mQ1uAk
nMYO5Jbwuk9r7Y6B4e68295tVoQcsunpczTXuG3pVTvaQrtL44JlOHMpvpslBFyO
QKF8no+oywqSZmZbZCGp6v4yBbxqfC9sRjQ+f5V/nr28YC+nktJf9ST7be9PvLtq
bUR/l8rdZ2kC/0uQE3wrrS8u2QM5l+N1IwIDAQABo1MwUTAdBgNVHQ4EFgQU5bT1
wyGUG6VOPu2B08AxRLfJs8kwHwYDVR0jBBgwFoAU5bT1wyGUG6VOPu2B08AxRLfJ
s8kwDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAH3oQ2/EvmXGH
JS1xcvia2YLAJSV1gqZQg5WqEZOqz0X47AicYRZrahpq6VdWRIzhAPBiIDMI37wC
Bf9JBL8/5Lk9eKMgMFxFLHoQmuwFaD40Ok0264QtqpCeb0cen/EnFBtIyHmvby9d
yBbuAmuwcWs7Dc8ItKxDUt9EHdr3ynhAZhtDaVZEFwrTYERvMb5J49k7OrU9+IBR
uNLDuUL4EPGk084uoa/rwQxUDwWQ05aw81c/Q0ssPeyekgLNfet4HX4lzBDJWZEQ
FUu9LuwG/tVRBIecvo/IcUuQ1/UObbRAXpp0Y8aO56UVeBvOb9bG2/wjRJmrgR+1
vCCFsBlglA==
-----END CERTIFICATE-----`)

var secret = &corev1.Secret{
	ObjectMeta: metav1.ObjectMeta{
		Name:      netcfg.ServingRoutingCertName,
		Namespace: system.Namespace(),
	},
	Data: map[string][]byte{
		certificates.CaCertName:     secretCA,
		certificates.PrivateKeyName: tlsKey,
		certificates.CertName:       tlsCrt,
	},
}

var validConfigmap = &corev1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "valid",
		Namespace: system.Namespace(),
		Labels: map[string]string{
			networking.TrustBundleLabelKey: "true",
		},
	},
	Data: map[string]string{
		certificates.CertName: string(configmapCA),
	},
}

var invalidConfigmap = &corev1.ConfigMap{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "invalid",
		Namespace: system.Namespace(),
		Labels: map[string]string{
			networking.TrustBundleLabelKey: "true",
		},
	},
	Data: map[string]string{
		certificates.CertName: "not a CA",
	},
}
