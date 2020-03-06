# Test files for metrics

The cert files were generated with:

```shell
openssl req -x509 -nodes -newkey dsa:<(openssl dsaparam 1024) -keyout client-key.pem -out client-cert.pem -days 10000
```

Note that there are some manual prompts later in the process. This seemed
simpler than generating the certs in Go.

To view the cert:

```shell
openssl x509 -noout -text -in client-cert.pem
```
