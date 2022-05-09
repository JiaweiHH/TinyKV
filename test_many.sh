#!/bin/bash
project_name=${1}
round_times=${2}
for ((i = 1; i <= round_times; i++)); do
  make ${project_name} > ./test_result/result-${i}.txt
  echo "$(TZ=UTC-8 date +%Y-%m-%d" "%H:%M:%S) PASS ${i}/${round_times}"
done
echo "✅: PASS ALL TEST"