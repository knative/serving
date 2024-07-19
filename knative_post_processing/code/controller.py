import os
import pandas as pd
import numpy as np
from pod_name_start_and_end_parser import get_pod_name_start_and_end_data
from request_id_start_and_end_parser import get_request_id_start_and_end_data
from request_time_distribution import get_request_time_distribution
from request_post_processing import plot
from get_es_result import gen_log_files
from get_kourier_logs import get_supplementary_request_data

CURRENT_RUN_KEYWORD = "femux_v3_mc"

DATA_FOLDER = f"../data/es_logs/{CURRENT_RUN_KEYWORD}/"
OUTPUT_FOLDER = f"../output/{CURRENT_RUN_KEYWORD}/"
PLOT_FOLDER = f"../plot/{CURRENT_RUN_KEYWORD}_v2/"
SUPPLEMENTARY_DATA_FILE = f"../data/fluentd/{CURRENT_RUN_KEYWORD}/0.log.20231127-000302"

if not os.path.exists(DATA_FOLDER):
    print("creating directory structure...")
    os.makedirs(DATA_FOLDER)

if not os.path.exists(OUTPUT_FOLDER):
    print("creating directory structure...")
    os.makedirs(OUTPUT_FOLDER)

minutes_since_start = 26*60 # minutes

EXPERIMENT_END_DURATION = minutes_since_start * 60 # seconds

gen_log_files(DATA_FOLDER, minutes_since_start)
get_pod_name_start_and_end_data(DATA_FOLDER, OUTPUT_FOLDER)
get_request_id_start_and_end_data(DATA_FOLDER, OUTPUT_FOLDER)

DATA_FILE_NAME = "pod_name_to_request_id_mapping.csv"
OUTPUT_FILE_NAME = "qp_df.pickle"

get_request_time_distribution(DATA_FOLDER, DATA_FILE_NAME, OUTPUT_FOLDER, OUTPUT_FILE_NAME)

POD_DATA_FILE = "pod_name_start_and_end_df.pickle"
REQ_DATA_FILE = "request_id_start_and_end_df.pickle"
TIMESTEP = 60 # buckets of TIMESTEP(s) worth of data

CUTOFF_TIME = 5 # minutes

if minutes_since_start < 2*CUTOFF_TIME:
    CUTOFF_TIME = 0

plot(OUTPUT_FOLDER, REQ_DATA_FILE, POD_DATA_FILE, PLOT_FOLDER, OUTPUT_FOLDER, TIMESTEP, CUTOFF_TIME)