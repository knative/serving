import os
import pandas as pd
import numpy as np

def get_supplementary_request_data(supplementary_data_file, output_folder):

    df = pd.read_csv(supplementary_data_file, delimiter='\t', header=None, names=['log'])
    output_df = pd.DataFrame(columns=["REQUEST_ID", "REQUEST_START_TIME", "REQUEST_DURATION", "REQUEST_RESPONSE_TYPE", "APP_NAME"])
    index = 0
    for _, row in df.iterrows():
        """
        ['2023-11-26T16:37:48.506309211Z', 'stdout', 'F', '[2023-11-26T16:37:41.484Z]', '"POST', '/hello', 'HTTP/1.1"', '200', '-', '2', '0', '173', '173', '"-"', '"python-requests/2.25.1"', '"85b57410-e984-49c8-90b2-cdac9fc61730"', '"application-ae0efe36.default.192.168.106.10.sslip.io"', '"10.233.102.189:8012"']
        """
        log_line_str = row["log"]
        if "POST" in log_line_str:
            log_line_list = log_line_str.split(' ')
            request_id = log_line_list[15][1:-1]
            start_time = log_line_list[3][1:-1]
            request_duration = log_line_list[11]
            request_response_type = log_line_list[7]
            app_name = log_line_list[16].split('.')[0][1:]
            output_df.loc[index] = [request_id, start_time, request_duration, request_response_type, app_name]
            index += 1

    # merge existing data with supplementary data
    request_df = pd.read_pickle(output_folder + "request_id_start_and_end_df.pickle")
    merged_df = output_df.set_index('REQUEST_ID').combine_first(request_df.set_index('REQUEST_ID')).reset_index()
    print(merged_df.to_string())

    merged_df.to_pickle(output_folder + "request_id_start_and_end_df.pickle")

if __name__ == '__main__':
    CURRENT_RUN_KEYWORD = "femux_v3_mc"
    OUTPUT_FOLDER = f"../output/{CURRENT_RUN_KEYWORD}/"
    SUPPLEMENTARY_DATA_FILE = f"../data/fluentd/{CURRENT_RUN_KEYWORD}/0.log.20231127-000302"

    get_supplementary_request_data(SUPPLEMENTARY_DATA_FILE, OUTPUT_FOLDER)
