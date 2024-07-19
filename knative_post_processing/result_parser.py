import math
import time
import re
import subprocess

import matplotlib.pyplot as plt

from helper import (
    get_concurrency_estimate,
    get_rpc_range,
    get_ideal_average_concurrency,
)

command = "kubectl -n knative-serving logs $(kubectl -n knative-serving get pods -l app=autoscaler -o name) -c autoscaler | tail -n 500000"  # Replace this with your desired command
run_type = "custom"
result = subprocess.run(command, shell=True, stdout=subprocess.PIPE, text=True)
output_logs = result.stdout

experiment_start_time = 1693272333.0
exec_time = 5

application_names = [
    "hello",
]

application_mem_map = {
    "hello": 300,
}

application_at_map = {"hello": [1.0]}

complete_application_names = [
    f"default/{app_name}-world" for app_name in application_names
]

all_app_traces = {}
num_subplots = len(application_names)
cs_list = []
wms_list = []
inv_list = []

pattern_heads = [
    "concStatTrace:",
    "mutatedConcStatTrace:",
    "readyPodCountTrace:",
    "timeStampTraceRpc:",
    "timestampTraceConc:",
]  # , 'cold_start_time_trace', 'wasted_memory_time_trace', 'dpcTrace:', 'readyPodCountTrace:'
for complete_app_name, app_name in zip(complete_application_names, application_names):
    map_for_plotting = {}
    for pattern_head in pattern_heads:
        curr_pattern = f'"{pattern_head}\[{complete_app_name}\]\s*\[([^\]]+)\]'
        matches = re.findall(curr_pattern, output_logs)
        if len(matches) != 0:
            extracted_lists = [list(map(float, match.split(" "))) for match in matches]
            map_for_plotting[pattern_head] = extracted_lists[-1]
    all_app_traces[f"{app_name}"] = map_for_plotting
    print(map_for_plotting, complete_app_name)
    if (
        "readyPodCountTrace:" in map_for_plotting
        and "timeStampTraceRpc:" in map_for_plotting
        and "timestampTraceConc:" in map_for_plotting
        and "concStatTrace:" in map_for_plotting
        and len(map_for_plotting) != 0
    ):
        rpc_timestamp = map_for_plotting["timeStampTraceRpc:"]
        rpc = map_for_plotting["readyPodCountTrace:"]

        conc_timestamp = map_for_plotting["timestampTraceConc:"]
        if run_type == "custom":
            # concurrency for cs should be the mutated one
            concTrace = map_for_plotting["mutatedConcStatTrace:"]
        else:
            concTrace = map_for_plotting["concStatTrace:"]

        at_trace = application_at_map[app_name]
        at_trace = [at + experiment_start_time for at in at_trace]

        map_at_to_ideal_avg_conc = get_ideal_average_concurrency(
            experiment_start_time, at_trace, exec_time, conc_timestamp
        )

        invocation_list = []
        cold_start_list = []
        wms_list = []
        print(
            len(at_trace),
            len(concTrace),
            len(conc_timestamp),
            len(rpc),
            len(rpc_timestamp),
        )
        # initial cold starts and invocations
        invocations = 0
        cold_starts = 0
        wasted_mem = 0
        index = 0

        # change per pod concurrency in case of different density of traces
        per_pod_concurrency = 10

        if at_trace[0] < rpc_timestamp[0]:
            first_invocation_time = at_trace[0]
            for curr_time in at_trace:
                if curr_time < rpc_timestamp[0]:
                    invocations += 1
                    index += 1
                    cold_starts += 1
            invocation_list.append(invocations)
            cold_start_list.append(cold_starts)
        # invocations and RPC start at the same time
        if index == len(at_trace):
            index = 0
        at_trace = at_trace[index:]
        print(
            f"inv list {invocation_list} and cold starts {cold_start_list}, index {index}"
        )

        for i in range(len(rpc_timestamp) - 1):
            list_of_invocations_between_a_period = []
            invocations = 0
            cold_starts = 0
            for j in range(len(at_trace)):
                # check between 2 successive rpc values (ideally 2 seconds) the number of invocation arrivals
                if (
                    rpc_timestamp[i] <= at_trace[j]
                    and at_trace[j] < rpc_timestamp[i + 1]
                ):
                    list_of_invocations_between_a_period.append(at_trace[j])
                    invocations += 1
            # if there are any invocations starting from the first invocation timestamp to timestamp + timedelta(seconds=0.033) constitute n cold starts
            if invocations != 0:
                for inv in list_of_invocations_between_a_period:
                    ideal_avg_conc = map_at_to_ideal_avg_conc[inv]
                    if rpc[i] == 0:
                        cold_starts += 1
                    elif math.ceil(ideal_avg_conc / per_pod_concurrency) > rpc[i]:
                        cold_starts += 1
                    print(
                        "approx conc: {} and rpc {} at index {} cs {} inv {}".format(
                            ideal_avg_conc, rpc[i], i, cold_starts, invocations
                        )
                    )
            invocation_list.append(invocations)
            cold_start_list.append(cold_starts)

        print(
            f"inv list {invocation_list} [{sum(invocation_list)}] and cold starts {cold_start_list} [{sum(cold_start_list)}]"
        )

        mem_per_pod = 0.300
        mem_allocated = application_mem_map[app_name] / 1000.0

        # concurrency for wms should be the normal one
        concTrace = map_for_plotting["concStatTrace:"]

        wasted_mem_list = []

        # wasted memory calculation
        for i in range(len(conc_timestamp) - 1):
            left_conc_time = conc_timestamp[i]
            right_conc_time = conc_timestamp[i + 1]
            wasted_mem = get_rpc_range(
                left_conc_time,
                right_conc_time,
                rpc_timestamp,
                conc_timestamp,
                concTrace,
                rpc,
                mem_per_pod,
                mem_allocated,
                per_pod_concurrency,
            )
            wasted_mem_list.append(wasted_mem)
        # print(f"wasted_mem {sum(wasted_mem_list)} {len(wasted_mem_list)}")
        cs_list.append(sum(cold_start_list))
        wms_list.append(sum(wasted_mem_list))
        inv_list.append(sum(invocation_list))
        print(
            f"per app inv {invocation_list}[{sum(invocation_list)}] cs {cold_start_list}[{sum(cold_start_list)}] wms {wasted_mem_list}[{sum(wasted_mem_list)}]"
        )
        print(f"time elapsed so far: {int(time.time()) - experiment_start_time}s")
        # plot
        plt.plot(
            conc_timestamp,
            [conc / per_pod_concurrency for conc in concTrace],
            label="conc",
        )
        plt.scatter(at_trace[:800], [1 for at in at_trace][:800], label="at")
        plt.plot(rpc_timestamp, rpc, label="rpc")
        plt.legend()
        print(len(concTrace), len(conc_timestamp))
        plt.savefig("output_files/cs_wm_snapshot.pdf")

if len(inv_list) != 0:
    print(
        f"invocations {inv_list} cold starts [{sum([(cs * 100 / inv) for cs, inv in zip(cs_list, inv_list)])/len(inv_list)}] wasted memory {wms_list}"
    )
