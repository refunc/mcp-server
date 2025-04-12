# Refunc MCP Server


## Deploy Refunc

Deploy all refunc components in namespace refunc-system.

```
kubectl apply -f https://raw.githubusercontent.com/refunc/mcp-server/refs/heads/main/deploy/play-all-in-one.yaml
```

Then proxy refunc service to local.

```
kubectl port-forward svc/aws-api 8000:http --address 0.0.0.0 -n refunc-system
kubectl port-forward svc/mcp-server 8001:http --address 0.0.0.0 -n refunc-system
```

PS: The data from the demo deployment is not persistent.

## Create Function

Create a Function with [refunc-cli](https://github.com/refunc/refunc-cli).

```
pip install -U refunc-cli
mkdir mcp-demo && cd mcp-demo
rfctl init

Initing function manifest in /path/mcp-demo/lambda.yaml
Name: mcp-demo
Namespace: refunc-system
Select Language:
1 - python
2 - go
Choose from 1, 2 [1]:
Select Runtime:
1 - python3.10
2 - python3.9
3 - python3.8
Choose from 1, 2, 3 [1]:

# write any code in main.py, the demo is echo a hello msg.

AWS_DEFAULT_ENDPOINT=http://127.0.0.1:8000 rfctl create
```

PS: You need a Python 3 environment.

## Create MCP Endpoint


Edit lambda.yaml as below code.

```

metadata:
  name: mcp-demo
  namespace: refunc-system
spec:
  build:
    source: .
    manifest: requirements.txt
    language: python
    architecture: x86_64
  handler: main.lambda_handler
  timeout: 120
  runtime: "python3.10"
  concurrency: 1
  environment:
    ENV_TEST: TEST
#  url:
#    cors:
#      allowCredentials: true
#      allowHeaders: "*"
#      allowMethods: "*"
#      allowOrigins: "*"
#      exposeHeaders: "*"
#      maxAge: 300
  events:
    # - name: hourly
    #   type: cron
    #   mapping:
    #     cron: 0 * * * *
    #     location: Asia/Shanghai
    #     args:
    #       var1: value1
    #     saveLog: false
    #     saveResult: false
    - name: mcp
      type: mcp
      mapping:
        args:
          token: mcp-demo
          tools:
            - name: echo-hello
              desc: Echo a hello msg #Cannot contain single quotes
              schema:
                type: object
                properties: {}
                required: []
        saveLog: false
        saveResult: false

```

```
AWS_DEFAULT_ENDPOINT=http://127.0.0.1:8000 rfctl update-config
```

The demo MCP SSE endpoint is: `http://127.0.0.1:8001/refunc-system/mcp-demo/test/mcp-demo/sse`

Refunc MCP SSE url path format is: `/namespace/<token-secret-name>/<token>/<func-name>/sse`


## MCP Event Spec

```
- name: mcp
  type: mcp # the event type must be mcp.
  mapping:
    args:
      token: mcp-demo # the token secret name you can find in play-all-in-one.yaml.
      tools: # mcp tools
        - name: echo-hello # mcp tool name
          desc: Echo a hello msg # mcp tool desc
          schema: # mcp tool parameters, described with a valid JSON Schema.
            type: object
            properties: {}
            required: []
    saveLog: false
    saveResult: false
```

The tools field of the event is a data structure, and you can implement multiple tools using a function.
When calling the function, refunc will add two built-in parameters: _call_type and _call_method, where _call_method is the name of the tool.