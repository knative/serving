import unittest

from helper import get_concurrency_estimate, get_rpc_range, get_ideal_average_concurrency


class TestPostProcessing(unittest.TestCase):

    def test_get_concurrency_estimate_case_4(self):
        rpc_lower_bound = 1
        rpc_upper_bound = 3
        rpc_timestamp = [1, 3]
        rpc = [1, 2]
        conc_trace = [1, 2]
        conc_timestamp = [0, 2]
        invocations = 2
        per_pod_concurrency = 1
        # case 4
        print(get_concurrency_estimate(rpc_lower_bound, rpc_upper_bound, rpc_timestamp, rpc, conc_trace, conc_timestamp, invocations, per_pod_concurrency), 1.0)

    def test_get_concurrency_estimate_case_1(self):
        rpc_lower_bound = 1
        rpc_upper_bound = 3
        rpc_timestamp = [1, 3]
        rpc = [1, 2]
        conc_trace = []
        conc_timestamp = []
        invocations = 2
        per_pod_concurrency = 1
        # case 1
        print(get_concurrency_estimate(rpc_lower_bound, rpc_upper_bound, rpc_timestamp, rpc, conc_trace, conc_timestamp, invocations, per_pod_concurrency), 2.0)

    def test_get_concurrency_estimate_case_2(self):
        rpc_lower_bound = 1
        rpc_upper_bound = 3
        rpc_timestamp = [1, 3]
        rpc = [1, 2]
        conc_trace = [2]
        conc_timestamp = [2]
        invocations = 2
        per_pod_concurrency = 1
        # case 2
        print(get_concurrency_estimate(rpc_lower_bound, rpc_upper_bound, rpc_timestamp, rpc, conc_trace, conc_timestamp, invocations, per_pod_concurrency), 1.0)

    def test_get_concurrency_estimate_case_3a(self):
        rpc_lower_bound = 1
        rpc_upper_bound = 3
        rpc_timestamp = [1, 3]
        rpc = [1, 2]
        conc_trace = [1]
        conc_timestamp = [0]
        invocations = 2
        per_pod_concurrency = 1
        # case 3a
        print(get_concurrency_estimate(rpc_lower_bound, rpc_upper_bound, rpc_timestamp, rpc, conc_trace, conc_timestamp, invocations, per_pod_concurrency), 2.0)
    
    def test_get_concurrency_estimate_case_3b(self):
        rpc_lower_bound = 3
        rpc_upper_bound = 5
        rpc_timestamp = [1, 3, 5]
        rpc = [1, 2, 2]
        conc_trace = [1, 2]
        conc_timestamp = [0, 2]
        invocations = 2
        per_pod_concurrency = 1
        # case 3b
        print(get_concurrency_estimate(rpc_lower_bound, rpc_upper_bound, rpc_timestamp, rpc, conc_trace, conc_timestamp, invocations, per_pod_concurrency), 0.0)

    def test_get_get_rpc_range_case_1(self):
        left_conc_time = 0
        right_conc_time = 2
        rpc_timestamp = [1, 3]
        rpc = [1, 2]
        conc_trace = [0, 2]
        conc_timestamp = [0, 2]
        invocations = 2
        per_pod_concurrency = 1
        mem_per_pod = 0.300
        mem_allocated = 0.300
        # case 1
        self.assertEqual(get_rpc_range(
            left_conc_time,
            right_conc_time,
            rpc_timestamp,
            conc_timestamp,
            conc_trace,
            rpc,
            mem_per_pod,
            mem_allocated,
            per_pod_concurrency,
        ), 0.3)

    def test_get_get_rpc_range_case_3(self):
        left_conc_time = 1
        right_conc_time = 3
        rpc_timestamp = [0, 2]
        rpc = [1, 2]
        conc_trace = [0, 2]
        conc_timestamp = [1, 3]
        invocations = 2
        per_pod_concurrency = 1
        mem_per_pod = 0.300
        mem_allocated = 0.300
        # case 3
        self.assertAlmostEqual(get_rpc_range(
            left_conc_time,
            right_conc_time,
            rpc_timestamp,
            conc_timestamp,
            conc_trace,
            rpc,
            mem_per_pod,
            mem_allocated,
            per_pod_concurrency,
        ), 0.9)

    def test_get_get_rpc_range_case_3(self):
        left_conc_time = 1
        right_conc_time = 3
        rpc_timestamp = [0, 2]
        rpc = [1, 2]
        conc_trace = [0, 2]
        conc_timestamp = [1, 3]
        invocations = 2
        per_pod_concurrency = 1
        mem_per_pod = 0.300
        mem_allocated = 0.300
        # case 3
        self.assertAlmostEqual(get_rpc_range(
            left_conc_time,
            right_conc_time,
            rpc_timestamp,
            conc_timestamp,
            conc_trace,
            rpc,
            mem_per_pod,
            mem_allocated,
            per_pod_concurrency,
        ), 0.9)


    def test_get_ideal_average_concurrency_case_1(self):
        experiment_start_time = 22
        at_trace = [22.1, 22.3]
        exec_time = 5
        conc_timestamp = [22, 24, 26]
        print(get_ideal_average_concurrency(experiment_start_time, at_trace, exec_time, conc_timestamp))

    def test_get_ideal_average_concurrency_case_2(self):
        experiment_start_time = 22
        iat_trace = [0.333,0.667,0.999,1.333,1.667,1.999,2.333,2.667,2.999,3.333,3.667,3.999,4.333,4.667,4.999,5.333,5.667,5.999,6.333,6.667,6.999,7.333,7.667,7.999,8.333,8.667,8.999,9.333,9.667,9.999,23.33]
        at_trace = [iat+experiment_start_time for iat in iat_trace]
        exec_time = 5
        conc_timestamp = [i for i in range(22, 48, 2)]
        print(get_ideal_average_concurrency(experiment_start_time, at_trace, exec_time, conc_timestamp))


if __name__ == '__main__':
    unittest.main()