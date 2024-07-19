
def step_pattern_generator():

    req_per_sec_list = [10, 30, 50, 70]

    retain_pattern_duration = 300 # sec

    interarrival_time_list = []

    for req_per_sec in req_per_sec_list:
        cur_pattern = [1/req_per_sec]*req_per_sec*retain_pattern_duration
        interarrival_time_list += cur_pattern
        
    return interarrival_time_list

def peak_trough_peak_generator():

    peak_req_per_sec = 70

    retain_peak_duration = 60 # sec

    retain_trough_duration = 120 # sec

    interarrival_time_list = [peak_req_per_sec]*retain_peak_duration + [120] + [peak_req_per_sec]*retain_peak_duration

    return interarrival_time_list

interarrival_time_list = step_pattern_generator()

# save the interarrival time list to a file
with open('interarrival_time_list.txt', 'w') as f:
    f.write("[")
    for item in interarrival_time_list:
        f.write("%s,\n" % item)
    f.write("]")