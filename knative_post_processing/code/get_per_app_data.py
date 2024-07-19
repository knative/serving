
from datetime import timedelta
import pandas as pd
from helper import app_to_exec



def is_cold_start(real_exec_duration, ideal_exec_duration, threshold):
    return True if real_exec_duration - ideal_exec_duration >= threshold else False

def is_cold_start_or_failed(real_exec_duration, ideal_exec_duration, threshold, response_type):
    return True if real_exec_duration - ideal_exec_duration >= threshold or response_type == "FAILED" else False

def get_per_app_data(threshold, output_folder, output_file_name, cutoff_time):
    df = pd.read_pickle(output_folder + "request_id_start_and_end_df.pickle")
    df = df[(df["REQUEST_START_TIME"] > df["REQUEST_START_TIME"].iloc[0] + timedelta(minutes=cutoff_time)) & (df["REQUEST_START_TIME"] < df["REQUEST_START_TIME"].iloc[-1] - timedelta(minutes=25*cutoff_time))]

    df = df[["REQUEST_ID", "APP_NAME", "REQUEST_DURATION", "REQUEST_RESPONSE_TYPE"]]

    df["COLD_START"] = df.apply(lambda row: is_cold_start(row.REQUEST_DURATION, app_to_exec(row.APP_NAME), threshold), axis=1)

    df["COLD_START_OR_FAILED"] = df.apply(lambda row: is_cold_start_or_failed(row.REQUEST_DURATION, app_to_exec(row.APP_NAME), threshold, row.REQUEST_RESPONSE_TYPE), axis=1)

    result_df = df.groupby('APP_NAME', as_index=False).agg(
        REQUEST_COUNT = ('REQUEST_ID', 'count'), 
        COLD_START_COUNT = ('COLD_START', lambda x: (x == True).sum()),
        COLD_START_OR_FAILED_COUNT = ('COLD_START_OR_FAILED', lambda x: (x == True).sum()),
        FAILED_REQUEST_COUNT = ('REQUEST_RESPONSE_TYPE', lambda x: (x == 'FAILED').sum()))
    # result_df["COLD_START_COUNT"] = result_df[["COLD_START_COUNT", "FAILED_REQUEST_COUNT"]].apply(lambda x: max(x.COLD_START_COUNT, x.FAILED_REQUEST_COUNT), axis=1)
    print(result_df)
    result_df.to_pickle(output_folder + output_file_name)


if __name__ == '__main__':
    THRESHOLD = 700
    CURRENT_RUN_KEYWORD = "femux_26_hr_63_100"
    OUTPUT_FOLDER = f"../output/{CURRENT_RUN_KEYWORD}/"
    OUTPUT_FILE = "per_application_data.pickle"

    CUTOFF_TIME = 5 # minutes

    get_per_app_data(THRESHOLD, OUTPUT_FOLDER, OUTPUT_FILE, CUTOFF_TIME)