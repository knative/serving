#/usr/bin/env bash

find ready -type f -not -name "*.png" -not -name "*.svg" -exec gnuplot -e "filename='{}'" -p readiness.gnuplot \;
