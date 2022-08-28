# modbus-poller

This package provides an interval poller for modbus data, with results available via http as a JSON object or inserted into an OpenTSDB
compatable endpoint.

I'm using this package to poll data from my solar power system to store in VictoriaMetrics and graphing with Grafana.

The config file is in JSON format, and the file included is my running config.

If you specify the Listen value, it will bind to the address/port you give (example: 127.0.0.1:8192). The poller will collect data at the specified interval, and include an average of the last x number of data points (where x is the value of AverageWindow in the config). For example, setting Interval to 2 and AverageWindow to 30, the json data will have both the latest poll and the average of the last 60 seconds.

If you specify the OpenTSDB value it will push data at the interval specified in the config.

Note that the interval is duration between when a poll completes and when the next begins.
