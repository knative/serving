
import yaml
import os
import time
import pandas as pd
import subprocess

iat_workload_data = "../data/meta_func_deployment_100/dataframe/workload_event_data_max_63_100_apps.pickle"

app_url_template = "application-{}.default.10.0.0.10.sslip.io"

image_template = "10.0.0.39:5000/{}.go"

def create_application_deployment_files(app_name, app_type, output_folder, cpu_alloc, memory_alloc, cushion_memory, per_pod_concurrency, image_str, node_count):
    # df = pd.read_pickle(iat_workload_data)

    # for ix, row in df.iterrows():
    #     app_to_exec_map[row["HashApp"]] = row["ExecDurations"]

    with open('../data/deployment_template.yaml', 'r') as deployment_file:
        try:
            app_template = yaml.safe_load(deployment_file)
            print("TEMPLATE:\n", app_template)
        except yaml.YAMLError as e:
            print(f"Exiting with error {e}")
        print(app_template)
        app_template['metadata']['name'] = app_name
        app_template['spec']['template']['spec']['containers'][0]['image'] = image_template.format(app_name.replace("-", "_"))
        app_template['spec']['template']['spec']['containers'][0]['resources']['requests']['cpu'] = f'{cpu_alloc*per_pod_concurrency}m'
        app_template['spec']['template']['spec']['containers'][0]['resources']['requests']['memory'] = f'{memory_alloc*per_pod_concurrency+cushion_memory}Mi'

        app_template['spec']['template']['spec']['containers'][0]['resources']['limits']['cpu'] = f'{cpu_alloc*per_pod_concurrency}m'
        app_template['spec']['template']['spec']['containers'][0]['resources']['limits']['memory'] = f'{memory_alloc*per_pod_concurrency+cushion_memory}Mi'


        if not os.path.isdir(output_folder):
            os.makedirs(output_folder)

        output_file_path = f'../output/{app_type}/{app_name}-world.yaml'
        
        if not os.path.isfile(output_file_path):
            open(output_file_path, 'w').close()
        
        with open(output_file_path, 'w') as output_file:
            yaml.safe_dump(app_template, output_file)


def deploy_app(app_type, output_folder):
    deployment_file_names = [f for f in os.listdir(output_folder) if f.endswith('.yaml')]
    print(deployment_file_names)
    for file in deployment_file_names:
        print(f"kn service create -f {output_folder}{file} --revision-name=world --force")
        command = f"kn service create -f {output_folder}{file} --revision-name=world --force"
        result = subprocess.run(command, shell=True, stdout=subprocess.PIPE, text=True)
        output_logs = result.stdout
        print(output_logs)


def create_deployment(df, app_type, output_folder, node_count, cushion_memory, per_pod_concurrency, cpu_per_app):
    if app_type == 'hello':
        create_application_deployment_files('hello', app_type, output_folder, cpu_per_app, cpu_per_app, cushion_memory, per_pod_concurrency, "nmunshi28/hello_buddy.go-30090a1a78cea440523bd0b47a0e0732:latest", node_count)
    else:
        for _, row in df.iterrows():
            app_name = f"application-{row.HashApp[:8]}"
            create_application_deployment_files(app_name, app_type, output_folder, cpu_per_app, row.AverageMemUsage, cushion_memory, per_pod_concurrency, app_url_template.format(app_name), node_count)
    deploy_app(app_type=app_type, output_folder=output_folder)



