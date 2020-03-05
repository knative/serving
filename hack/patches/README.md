# Patch file

These are applied after update-deps.sh pulls generated dependencies

---

## dryruncreate.patch

- Adds CreateWithOptions function to the k8s client
- [Commit added to upstream k8s](https://github.com/kubernetes/client-go/commit/e7a922c979d0f5cd6131039b2c86af97a164c7e4#diff-15cf3927b2b02b300f0305c68c6b244al)
- [Patch removal tracking issue](https://github.com/knative/serving/issues/7143)
