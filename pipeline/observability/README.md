# 📊 Observability Stack

Post deployment of Opentelemetry instrumented app using device agent and installing observability services at both WFM and device, OTEL app starts sending telemetry to collector running on device node.

## 🏗️ Stack Details 

### 🖥️ WFM Node
- 🔍 **Jaeger** - Distributed tracing
- 📈 **Prometheus** - Metrics collection and storage
- 📊 **Grafana** - Visualization and dashboards
- 📝 **Loki** - Log aggregation

### 📱 Device Node
- 🚀 **OTEL Instrumented App** - Application with telemetry
- 🔄 **OTEL Collector** - Telemetry data collection
- 📋 **Promtail** - Log shipping agent

## 🌐 Access URLs

The following URLs provide access to app telemetry (traces, metrics, and logs) through the observability tools installed on the WFM node:

### Traces on Jaeger
    1.	Hit the following URL http://<<WFM-Node-IP>>:32500/
    2.	Select the service name from dropdown
        

### Metrics on Prometheus
    1.	Hit the following URL http://<<WFM-Node-IP>>: 30900/query
    2.	Search the metric with name orders_processed_total
        

### Logs on grafana using loki as datasource
    1.	Hit following URL http://<<WFM-Node-IP>>: 32000
    2.	Configure loki as datasource. Add Loki URL and click on 'submit & test'.
    3.	On explore tab search with {pod="go-otel-service-856b66c9b9-rdb67"} (OTEL collector pod Name)
        
