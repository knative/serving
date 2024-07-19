from create_app_deployment_yaml import create_deployment
from app_deployment import deploy_apps
import pandas as pd
import os
import subprocess

APP_TYPE = "100_meta_func"

OUTPUT_FOLDER = f"../output/{APP_TYPE}/"

NODE_COUNT = 10

CUSHION_MEMORY = 50 # MB

PER_POD_CONCURRENCY = 1

CPU_PER_APP = 350 # millicores

SCALING_FACTOR = 1

df = deploy_apps(workload_scaling_factor=SCALING_FACTOR)

create_deployment(df=df, app_type=APP_TYPE, output_folder=OUTPUT_FOLDER, node_count=NODE_COUNT, cushion_memory=CUSHION_MEMORY, per_pod_concurrency=PER_POD_CONCURRENCY, cpu_per_app=CPU_PER_APP)