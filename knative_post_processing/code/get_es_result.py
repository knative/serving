import os
import pandas as pd
import numpy as np
from elasticsearch import Elasticsearch
from datetime import datetime, timedelta


REQUESTED_SIZE = 15000
NUM_BATCH_MINUTES = 10 # batch logs every 20 minutes

settings = {
    "index.max_result_window": REQUESTED_SIZE  # Set the desired value for max_result_window
}

json = {
  "track_total_hits": True,
  "sort": [
    {
      "@timestamp": {
        "order": "desc",
        "unmapped_type": "boolean"
      }
    }
  ],
  "fields": [
    {
      "field": "*",
      "include_unmapped": "true"
    },
    {
      "field": "@timestamp",
      "format": "strict_date_optional_time"
    },
    {
      "field": "kubernetes.annotations.client_knative_dev/updateTimestamp",
      "format": "strict_date_optional_time"
    },
    {
      "field": "kubernetes.annotations.kubernetes_io/config_seen",
      "format": "strict_date_optional_time"
    },
    {
      "field": "time",
      "format": "strict_date_optional_time"
    },
    {
      "field": "timestamp",
      "format": "strict_date_optional_time"
    },
    {
      "field": "ts",
      "format": "strict_date_optional_time"
    }
  ],
  "size": REQUESTED_SIZE,
  "version": True,
  "script_fields": {},
  "stored_fields": [
    "*"
  ],
  "runtime_mappings": {},
  "_source": False,
  "query": {
    "bool": {
      "must": [],
      "filter": [
        {
          "multi_match": {
            "type": "phrase",
            "query": "python-requests/2.25.1",
            "lenient": True
          }
        },
        {
          "range": {
            "@timestamp": {
              "format": "strict_date_optional_time",
              "gte": "2023-11-27T08:01:36.135Z",
              "lte": "2023-11-27T17:01:19.393Z"
            }
          }
        }
      ],
      "should": [],
      "must_not": []
    }
  },
}

filename_query_pairs = [("pod_name_to_request_id_mapping", "python-requests/2.25.1"), 
                    ("pod_name_start_time", "this is the start time"), 
                    ("pod_name_end_time", "SuccessfulDelete")]
cols = ["@timestamp","kubernetes.host","kubernetes.pod_name","log","message","knative_dev/pod"]

def gen_log_files(save_dir, minutes_since_start):
    es = Elasticsearch(hosts="http://localhost:9200")

    es.indices.put_settings(body=settings)
    print(f"size successfuly increased to {REQUESTED_SIZE}")

    start_time = datetime.utcnow() - timedelta(minutes=minutes_since_start)
    # need to batch logs to not go OOM when fetching them 
    for filename, query in filename_query_pairs:
      dfs = []
      for cur_start_time in range(0, minutes_since_start, NUM_BATCH_MINUTES):
        cur_end_time = cur_start_time + NUM_BATCH_MINUTES

        json["query"]["bool"]["filter"][1]["range"]["@timestamp"]["gt"] = (start_time + timedelta(minutes=cur_start_time)).isoformat() + 'Z'
        json["query"]["bool"]["filter"][1]["range"]["@timestamp"]["lte"] = (start_time + timedelta(minutes=cur_end_time)).isoformat() + 'Z'
        json["query"]["bool"]["filter"][0]["multi_match"]["query"] = query

        res = es.search(body=json, request_timeout=300)

        df = get_data(res)
        if len(df) != 0:
          result = df.apply(lambda row: [x[0] if isinstance(x, list) and len(x) > 0 else x for x in row], axis=1)
          df = pd.DataFrame(result.tolist(), columns=df.columns)
        df.drop_duplicates(inplace=True, ignore_index=True)
        dfs.append(df)

      combined_df = pd.concat(dfs, ignore_index=True)
      combined_df.to_csv(save_dir + filename + ".csv")


def get_data(res):
    col_dict = {col:[] for col in cols}

    for check in res["hits"]["hits"]:
        fields = check["fields"]
    
        for col in cols:
            if col in fields:
                col_dict[col].append(fields[col])
            else:
                col_dict[col].append(None)

    return pd.DataFrame(col_dict)


if __name__ == "__main__":
  save_dir = "../data/es_logs/"
    
  gen_log_files(save_dir, minutes_since_start=1080)