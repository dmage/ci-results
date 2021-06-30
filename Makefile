release: release-backend release-frontend

release-backend:
	docker build -t quay.io/rh-obulatov/ci-results:backend .
	docker push quay.io/rh-obulatov/ci-results:backend

release-frontend:
	docker build -t quay.io/rh-obulatov/ci-results:frontend ./frontend
	docker push quay.io/rh-obulatov/ci-results:frontend
