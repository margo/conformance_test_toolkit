FROM busybox:1.36.1

COPY test-suites /test-suites

CMD ["sh", "-c", "rm -rf /shared/* && cp -a /test-suites/. /shared/ && echo 'test-suites copied to the shared volume'"]
