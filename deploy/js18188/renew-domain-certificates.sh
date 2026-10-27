#!/bin/sh
set -eu
# Only reload after a successful renewal. nginx -t failure leaves current
# workers serving their existing configuration/certificates.
/usr/sbin/nginx -t
/bin/systemctl reload nginx
