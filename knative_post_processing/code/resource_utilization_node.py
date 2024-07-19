import os
import time
import math
import subprocess
import pandas as pd
import matplotlib.pyplot as plt
from datetime import datetime, timezone

from helper import application_names_to_mem

# get resource utilization across all pods
def get_resource_utilization(command_phrase, cushion_memory, per_pod_concurrency, cpu_alloc):
    """ Log resource usage and associated UTC timestamp in seconds

    returns:
        cpu : int (for pods) and [int, int, int] (for nodes)
            for pods, the int represents sum of usage in millicores.
            for nodes, the ints represent utilization percentage with i=0 for the control nodes, 
            i=1 for max worker, i=2 for min worker

        mem : int (for pods) and [int, int, int] (for nodes)
            same as cpu but with MiB
        
        time: float
    """

    if "node" in command_phrase:
        command_phrase = "pods"

        # get node to pod name mapping
        command = f"kubectl get pods -o wide"

        # get the output from the command
        output = subprocess.check_output(command, shell=True, text=True)

        if output == "No resources found in default namespace." or "error" in output:
            print("no pods are up!")
            return 0, 0, time.time()
        
        # remove first line as it only provides column name data
        line_output = output.split("\n")[1:-1]
        
        node_to_pod_name_map = {}

        for line in line_output:
            
            metric_list = line.split()

            if len(metric_list) != 0 and "application-" in metric_list[0]:
                node_name = metric_list[-3]
                pod_name = metric_list[0]

                if node_name in node_to_pod_name_map:
                    node_to_pod_name_map[node_name].append(pod_name)
                else:
                    node_to_pod_name_map[node_name] = [pod_name]

        command = f"kubectl top {command_phrase}"

        # get the output from the command
        output = subprocess.check_output(command, shell=True, text=True)

        # split into CPU and Memory
        if output == "No resources found in default namespace." or "error" in output:
            print("no pods are up!")
            return 0, 0, time.time()
        
        # remove first line as it only provides column name data
        line_output = output.split("\n")[1:-1]

        node_to_cpu_mem_map = {}

        app_name_to_pod_count_map = {}

        for line in line_output:
            
            metric_list = line.split()

            util_cpu = 0
            util_memory = 0
            
            if len(metric_list) != 0 and "application-" in metric_list[0]:

                util_cpu = int(metric_list[-2][:-1])
                util_memory = int(metric_list[-1][:-2])

                app_name = metric_list[0][:20]

                pod_name = metric_list[0]

                for node_name, pod_list in node_to_pod_name_map.items():
                    if pod_name in pod_list:
                        if node_name in node_to_cpu_mem_map:
                            node_to_cpu_mem_map[node_name][0] += util_cpu
                            node_to_cpu_mem_map[node_name][1] += util_memory
                        else:
                            node_to_cpu_mem_map[node_name] = [util_cpu, util_memory]

                if app_name in app_name_to_pod_count_map:
                    app_name_to_pod_count_map[app_name] += 1
                else:
                    app_name_to_pod_count_map[app_name] = 1

        total_util_cpu = 0
        total_util_memory = 0
        print(node_to_cpu_mem_map)
        for _, cpu_mem_list in node_to_cpu_mem_map.items():
            total_util_cpu += cpu_mem_list[0]
            total_util_memory += cpu_mem_list[1]

        # get alloc for nodes
        alloc_cpu = 0
        alloc_memory = 0
        for node_name, pod_list in node_to_pod_name_map.items():
            alloc_cpu += (cpu_alloc)*len(pod_list)
            print(node_name, cpu_alloc, len(pod_list))
            for pod_name in pod_list:
                alloc_memory += (application_names_to_mem[pod_name[:20]]*per_pod_concurrency + cushion_memory)
        
        cpu = [total_util_cpu, alloc_cpu]
        memory = [total_util_memory, alloc_memory]
    return cpu, memory, datetime.now(timezone.utc).timestamp(), app_name_to_pod_count_map
    
def get_application_allocation(app_to_util, cushion_memory):
    memory_wasted_across_apps = {}
    for key, val in app_to_util.items():
        
        # get number of pods per function
        number_of_pods_per_app = val[2]
        memory_utilization_per_app = val[1]
        
        # multiply with actual allocation per pod
        memory_allocated_per_pod = application_names_to_mem[key] + cushion_memory
        memory_allocated_per_app = memory_allocated_per_pod * number_of_pods_per_app

        # subtract allocated from actual usage
        memory_wasted_per_app = memory_allocated_per_app - memory_utilization_per_app

        # TODO: remove this check once the cushion_memory has a fixed value
        if memory_wasted_per_app < 0:
            raise Exception("wasted memory cannot be negative") 

        if key in memory_wasted_across_apps:
            memory_wasted_across_apps[key] += memory_wasted_per_app
        else:
            memory_wasted_across_apps[key] = memory_wasted_per_app
    return memory_wasted_across_apps

def collect_resource_utilization(command_substring, kubernetes_resource_type, cushion_memory, per_pod_concurrency, cpu_alloc, OUTPUT_FOLDER):
    """Store memory and cpu usage with associated UTC timestamp in seconds.
    """
    df = pd.DataFrame(columns=['COLLECTION_TIME', 'CPU_UTIL', 'CPU_ALLOC', 'CPU_PCT', 'MEM_UTIL', 'MEM_ALLOC', 'MEM_PCT'])
    cpu_list = []
    memory_list = []
    collection_time_list = []
    save_file_timer = 30 #s
    iter = 0
    try:
        while True:
            cpu, memory, collection_time, app_name_to_pod_count_map = get_resource_utilization(command_substring, cushion_memory, per_pod_concurrency, cpu_alloc)
            
            current_entry = {'COLLECTION_TIME': collection_time, 'CPU_UTIL': cpu[0], 'CPU_ALLOC': cpu[1], 'CPU_PCT': (cpu[0] * 100) / 36000.0, 'MEM_UTIL': memory[0], 'MEM_ALLOC': memory[1], 'MEM_PCT': (memory[0] * 100) / 144000.0, 'POD_COUNT': sum([pod_count_per_app for pod_count_per_app in app_name_to_pod_count_map.values()])}

            df = pd.concat([df, pd.DataFrame([current_entry])], ignore_index=True)
            print(cpu)
            print(memory)
            print(collection_time)
            print(df)
            cpu_list.append(cpu)
            memory_list.append(memory)
            collection_time_list.append(collection_time)
            # sleep for a 1 second
            time.sleep(1)
            iter += 1
            if not os.path.exists(OUTPUT_FOLDER):
                print("creating directory structure...")
                os.makedirs(OUTPUT_FOLDER)
            if iter == save_file_timer:
                iter = 0
                df.to_pickle(OUTPUT_FOLDER + "node_resource_data.pickle")

    except KeyboardInterrupt:
        print("\nStopped Monitoring...")
        

if __name__ == "__main__":
    cushion_memory = 50 # MB
    per_pod_concurrency = 1
    cpu_alloc = 350 # milli cores
    pod_mode = "utilization" # utilization or allocation

    CURRENT_RUN_KEYWORD = "femux_v3_mc"
    OUTPUT_FOLDER = f"../output/{CURRENT_RUN_KEYWORD}/"

    collect_resource_utilization("nodes", "all namespaces (c15)", cushion_memory, per_pod_concurrency, cpu_alloc, OUTPUT_FOLDER)