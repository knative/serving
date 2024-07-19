
import pandas as pd
import matplotlib.pyplot as plt

node_mode=False

if node_mode==True:
    df = pd.read_pickle("../output/resource_data/node_resource_data.pickle")

    plt.plot(df["COLLECTION_TIME"], df["CPU_PCT"], label="util_cpu")
    plt.plot(df["COLLECTION_TIME"], df["MAX_CPU_PCT"], label="max_cpu")
    plt.plot(df["COLLECTION_TIME"], df["MIN_CPU_PCT"], label="min_cpu")
    plt.legend()
    plt.savefig("../output/resource_data/node_cpu_util.pdf")
    plt.close()

    plt.plot(df["COLLECTION_TIME"], df["MEM_PCT"], label="util_mem")
    plt.plot(df["COLLECTION_TIME"], df["MAX_MEM_PCT"], label="max_mem")
    plt.plot(df["COLLECTION_TIME"], df["MIN_MEM_PCT"], label="min_mem")
    plt.legend()
    plt.savefig("../output/resource_data/node_mem_util.pdf")
else:
    df = pd.read_pickle("../output/resource_data/pod_resource_data.pickle")

    plt.plot(df["COLLECTION_TIME"], df["CPU_UTIL"], label="cpu_usage")
    plt.plot(df["COLLECTION_TIME"], df["CPU_ALLOC"], label="cpu_alloc")
    plt.legend()
    plt.savefig("../output/resource_data/pod_cpu_util.pdf")
    plt.close()

    plt.plot(df["COLLECTION_TIME"], df["MEM_UTIL"], label="mem_usage")
    plt.plot(df["COLLECTION_TIME"], df["MEM_ALLOC"], label="mem_alloc")
    plt.legend()
    plt.savefig("../output/resource_data/pod_mem_util.pdf")