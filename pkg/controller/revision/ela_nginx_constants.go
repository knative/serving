/*
Copyright 2017 The Kubernetes Authors.

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

package revision

import "text/template"

var nginxConfigTemplate *template.Template

type nginxConfigContext struct {
	EnableQueue bool
}

const nginxConfigTemplateSource = `
daemon off;

worker_processes auto;

events {
  worker_connections 4096;
  multi_accept on;
}

http {
  include mime.types;
  server_tokens off;

  variables_hash_max_size 2048;

  # set max body size to 32m as appengine supports.
  client_max_body_size 32m;

  tcp_nodelay on;
  tcp_nopush on;

  # GCLB uses a 10 minutes keep-alive timeout. Setting it to a bit more here
  # to avoid a race condition between the two timeouts.
  keepalive_timeout 650;
  keepalive_requests 10000;
  upstream queue { server 127.0.0.1:8012; }

  geo $source_type {
    default ext;
    127.0.0.0/8 lo;
    169.254.0.0/16 sb;

    35.191.0.0/16 lb;

    130.211.0.0/22 lb;

    172.16.0.0/12 do;
  }

  map $http_upgrade $ws_connection_header_value {
    default "";
    websocket upgrade;
  }

  server {

    listen 8180;
    # self signed ssl for load balancer traffic
    # vaikas commented these nex lines out
    #    listen 8443 default_server ssl;
    #    ssl_certificate /etc/ssl/localcerts/lb.crt;
    #    ssl_certificate_key /etc/ssl/localcerts/lb.key;
    #    ssl_protocols TLSv1.2;
    #    ssl_ciphers EECDH+AES256:!SHA1;
    #    ssl_prefer_server_ciphers on;
    #    ssl_session_timeout 3h;

    # Allow more space for request headers.
    large_client_header_buffers 4 32k;

    # Allow more space for response headers.
    proxy_buffer_size 64k;
    proxy_buffers 32 4k;
    proxy_busy_buffers_size 72k;

    # If version header present, make sure it's correct.

    if ($http_x_appengine_version !~ '(?:^$)|(?:^20170901t154638(?:\..*)?$)') {
      return 444;
    }

    set $x_forwarded_for_test "";

    # If request comes from sb, lo, or do, do not care about x-forwarded-for header.
    if ($source_type !~ sb|lo|do) {
      set $x_forwarded_for_test $http_x_forwarded_for;
    }

    # For local health checks only.
    if ($http_x_google_vme_health_check = 1) {
      set $x_forwarded_for_test "";
    }

    # If the request does not come from sb, strip trusted appengine headers if
    # it did not pass through the trusted appengine pipeline.
    if ($x_forwarded_for_test !~ '(?:^$)|(?:\b(?:10\.0\.0\.1),\s*\d+(?:\.\d+){3}$)') {
      set $http_x_appengine_api_ticket "";

      set $http_x_appengine_auth_domain "";

      set $http_x_appengine_blobchunksize "";

      set $http_x_appengine_blobsize "";

      set $http_x_appengine_blobupload "";

      set $http_x_appengine_cron "";

      set $http_x_appengine_current_namespace "";

      set $http_x_appengine_datacenter "";

      set $http_x_appengine_default_namespace "";

      set $http_x_appengine_default_version_hostname "";

      set $http_x_appengine_federated_identity "";

      set $http_x_appengine_federated_provider "";

      set $http_x_appengine_https "";

      set $http_x_appengine_inbound_appid "";

      set $http_x_appengine_inbound_user_email "";

      set $http_x_appengine_inbound_user_id "";

      set $http_x_appengine_inbound_user_is_admin "";

      set $http_x_appengine_queuename "";

      set $http_x_appengine_request_id_hash "";

      set $http_x_appengine_request_log_id "";

      set $http_x_appengine_tasketa "";

      set $http_x_appengine_taskexecutioncount "";

      set $http_x_appengine_taskname "";

      set $http_x_appengine_taskretrycount "";

      set $http_x_appengine_taskretryreason "";

      set $http_x_appengine_upload_creation "";

      set $http_x_appengine_user_email "";

      set $http_x_appengine_user_id "";

      set $http_x_appengine_user_is_admin "";

      set $http_x_appengine_user_nickname "";

      set $http_x_appengine_user_organization "";

    }

    location / {
      proxy_pass http://queue;
      proxy_redirect off;
      proxy_http_version 1.1;
      proxy_set_header Connection "";
      proxy_set_header Host $host;
      proxy_set_header X-Forwarded-Host $server_name;
      proxy_send_timeout 3600s;
      proxy_read_timeout 3600s;

      proxy_set_header X-AppEngine-Api-Ticket $http_x_appengine_api_ticket;

      proxy_set_header X-AppEngine-Auth-Domain $http_x_appengine_auth_domain;

      proxy_set_header X-AppEngine-BlobChunkSize $http_x_appengine_blobchunksize;

      proxy_set_header X-AppEngine-BlobSize $http_x_appengine_blobsize;

      proxy_set_header X-AppEngine-BlobUpload $http_x_appengine_blobupload;

      proxy_set_header X-AppEngine-Cron $http_x_appengine_cron;

      proxy_set_header X-AppEngine-Current-Namespace $http_x_appengine_current_namespace;

      proxy_set_header X-AppEngine-Datacenter $http_x_appengine_datacenter;

      proxy_set_header X-AppEngine-Default-Namespace $http_x_appengine_default_namespace;

      proxy_set_header X-AppEngine-Default-Version-Hostname $http_x_appengine_default_version_hostname;

      proxy_set_header X-AppEngine-Federated-Identity $http_x_appengine_federated_identity;

      proxy_set_header X-AppEngine-Federated-Provider $http_x_appengine_federated_provider;

      proxy_set_header X-AppEngine-Https $http_x_appengine_https;

      proxy_set_header X-AppEngine-Inbound-AppId $http_x_appengine_inbound_appid;

      proxy_set_header X-AppEngine-Inbound-User-Email $http_x_appengine_inbound_user_email;

      proxy_set_header X-AppEngine-Inbound-User-Id $http_x_appengine_inbound_user_id;

      proxy_set_header X-AppEngine-Inbound-User-Is-Admin $http_x_appengine_inbound_user_is_admin;

      proxy_set_header X-AppEngine-QueueName $http_x_appengine_queuename;

      proxy_set_header X-AppEngine-Request-Id-Hash $http_x_appengine_request_id_hash;

      proxy_set_header X-AppEngine-Request-Log-Id $http_x_appengine_request_log_id;

      proxy_set_header X-AppEngine-TaskETA $http_x_appengine_tasketa;

      proxy_set_header X-AppEngine-TaskExecutionCount $http_x_appengine_taskexecutioncount;

      proxy_set_header X-AppEngine-TaskName $http_x_appengine_taskname;

      proxy_set_header X-AppEngine-TaskRetryCount $http_x_appengine_taskretrycount;

      proxy_set_header X-AppEngine-TaskRetryReason $http_x_appengine_taskretryreason;

      proxy_set_header X-AppEngine-Upload-Creation $http_x_appengine_upload_creation;

      proxy_set_header X-AppEngine-User-Email $http_x_appengine_user_email;

      proxy_set_header X-AppEngine-User-Id $http_x_appengine_user_id;

      proxy_set_header X-AppEngine-User-Is-Admin $http_x_appengine_user_is_admin;

      proxy_set_header X-AppEngine-User-Nickname $http_x_appengine_user_nickname;

      proxy_set_header X-AppEngine-User-Organization $http_x_appengine_user_organization;

      proxy_set_header X-AppEngine-Version "";

      location = /_ah/health {
        if ( -f /tmp/health-checks/lameducked ) {
          return 503 'lameducked';
        }

        if ( -f /tmp/health-checks/app_lameducked ) {
          return 503 'app lameducked';
        }


        if ( !-f /var/lib/google/ae/disk_not_full ) {
          return 503 'disk full';
        }

        proxy_intercept_errors on;
        proxy_pass http://queue;
        proxy_redirect off;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Host $server_name;
        proxy_send_timeout 3600s;
        proxy_read_timeout 3600s;

        error_page 301 302 303 307 401 402 403 404 405 =200 /_ah/vm_health;
      }

      location = /_ah/vm_health {
        # TODO: This needs to be a separate directory from the
        # nginx.conf, since it's mounted as a configmap volume.
        if ( -f /tmp/health-checks/lameducked ) {
          return 503 'lameducked';
        }

        if ( -f /tmp/health-checks/app_lameducked ) {
          return 503 'app lameducked';
        }

        if ( !-f /var/lib/google/ae/disk_not_full ) {
          return 503 'disk full';
        }

        return 200 'ok';
      }
    }

    include /var/lib/nginx/extra/*.conf;
  }
  server {
    # expose /nginx_status but on a different port (8090) to avoid
    # external visibility / conflicts with the app.
    listen 8090;
    location /nginx_status {
      stub_status on;
      access_log off;
    }
    location / {
      root /dev/null;
    }
  }

  # Add session affinity entry to log_format line i.i.f. the GCLB cookie
  # is present.
  map $cookie_gclb $session_affinity_log_entry {
    '' '';
    default sessionAffinity=1;
  }

  # Output nginx access logs in the standard format, plus additional custom
  # fields containing "X-Cloud-Trace-Context" header, the current epoch
  # timestamp, the request latency, and "X-Forwarded-For" at the end.

  log_format custom '$remote_addr - $remote_user [$time_local] '
                    '"$request" $status $body_bytes_sent '
                    '"$http_referer" "$http_user_agent" '
                    'tracecontext="$http_x_cloud_trace_context" '
                    'timestampSeconds="${msec}000000" '
                    'latencySeconds="$request_time" '
                    'x-forwarded-for="$http_x_forwarded_for" '
                    'upgrade="$http_upgrade" '
                    '$session_affinity_log_entry';

  access_log /var/log/nginx/access.log custom;
  error_log /var/log/nginx/error.log warn;
}
`

func init() {
	nginxConfigTemplate = template.Must(template.New("nginxConfig").Parse(nginxConfigTemplateSource))
}
