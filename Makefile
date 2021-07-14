stack: build
	docker stack deploy -c stack.yml my-stack

build:
	docker build -t manager manager/
	docker build -t lb loadbalancer/

clean:
	docker stack rm my-stack || true
