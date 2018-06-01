# Code layout - Resources

* ./pkg/apis/serving/v1alpha1/*_types.go
* ./hack/update_codegen.sh when updating types fields
  * codegenerates
  * ./pkg/apis/serving/v1alpha1/zz_generated*
  * ./pkg/client/ 
* ./pkg/apis/{istio,cloudbuild}
  * Imported to help with interacting with these resources
