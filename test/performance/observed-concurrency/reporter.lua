-- Copyright 2018 The Knative Authors
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     https://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

-- parses a response as returned by the system under test
function parse_response(res)
  if (res == nil or res == '') then
    print('ERROR: Empty response from the test.')
    os.exit()
  end
  local split_at, _ = res:find(',', 0)
  if (split_at == nil or split_at == '') then
    print(string.format("ERROR: Malformed response '%s' from the test", res))
    os.exit()
  end
  local start_ts = tonumber(res:sub(0, split_at-1))
  local end_ts = tonumber(res:sub(split_at+1))
  return start_ts, end_ts
end

-- converts nanoseconds to milliseconds
function nanos_to_millis(n)
  return math.floor(n / 1000000)
end

-- returns the time it took to scale up to a certain scale
-- returns -1 if that scale was never reached
function time_to_scale(accumulated, scale)
  for _, result in pairs(accumulated) do
    if result['concurrency'] >= scale then
      return result['time']
    end
  end
  return -1
end


local threads = {}

function setup(thread)
  table.insert(threads, thread)
  if tonumber(os.getenv('concurrency')) == nil then
    print('ERROR: "concurrency" environment variable must be defined and be a number. Set it to the number of connections configured for wrk.')
    os.exit()
  end
end

function init(args)
  starts = {}
  ends = {}
end

function response(status, headers, body)
  start_ts, end_ts = parse_response(body)

  table.insert(starts, nanos_to_millis(start_ts))
  table.insert(ends, nanos_to_millis(end_ts))
end

function done(summary, latency, requests)
  local results = {}

  for _, thread in pairs(threads) do
    for _, start_ts in pairs(thread:get('starts')) do
      table.insert(results, {time=start_ts, concurrency_modifier=1})
    end

    for _, end_ts in pairs(thread:get('ends')) do
      table.insert(results, {time=end_ts, concurrency_modifier=1})
    end
  end

  table.sort(results, function(e1, e2) return e1['time'] < e2['time'] end)

  local accumulated_results = {}
  local start_time = results[1]['time']
  local current_time = -1
  local current_concurrency = 0
  local bucket_interval = 50

  for _, result in pairs(results) do
    local result_time = result['time']
    local concurrency_modifier = result['concurrency_modifier']
    current_concurrency = current_concurrency + concurrency_modifier

    if current_time - bucket_interval <= result_time and current_time + bucket_interval >= result_time then
      -- accumulate while still inside the bucket interval
      accumulated_results[-1]['concurrency'] = current_concurrency
    else
      -- create a new entry if outside the bucket interval
      table.insert(accumulated_results, {time=result_time - start_time, concurrency=current_concurrency})
    end
  end

  -- caveat: need to set the target concurrency as an environment variables
  -- even though it should be the very same as the number of connections passed
  -- to wrk. That option is not available here.
  local concurrency = tonumber(os.getenv('concurrency'))

  print()
  print('Scaling statistics:')
  print('  Time to scale (to ' .. concurrency .. '): ' .. time_to_scale(accumulated_results, concurrency) .. ' ms')
  print('  Time to scale breakdown:')

  for scale = 1,concurrency,1 do
    print('    ' .. scale .. ': ' .. time_to_scale(accumulated_results, scale) .. ' ms')
  end
end
