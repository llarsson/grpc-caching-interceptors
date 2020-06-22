# gRPC caching interceptors

Interceptors used to make gRPC caching-aware. Used in "Towards soft circuit breaking in service meshes via caching using dynamically estimated TTLs".

The `client/` directory contains the interceptor you want to use to get a simple TTL-abiding Cache component. See the [Value Service Caching Component](https://github.com/llarsson/value-service-caching) repo for how to use the code. You may want to use the reverse proxy that [our modified Protobuf compiler](https://github.com/llarsson/protobuf) gives you, but should not have to.

The `server/` directory contains the interceptor that lets you estimate how long a response is valid. You can affect how this estimate is produced by setting the following environment variables for your program that includes the interceptor:

 * `PROXY_CACHE_BLACKLIST` should be a regular expression that blacklists operations in your gRPC service from caching (they will not be assigned a caching header, and thus, not cached).
 * `PROXY_MAX_AGE` should be set to one of the following values (if not possible to parse, the Estimator will act in pass-through mode and just not assign a TTL to responses):
   * `static-N`, where `N` is the number of seconds to statically always respond with, e.g., `static-10` for 10 second TTL for every response object.
	 * `dynamic-adaptive-N`, where N is the parameter to the Adaptive TTL algorithm (read the paper).
	 * `dynamic-updaterisk-N`, where N is the parameter to the Update-risk based algorithm (read the paper).

See the [Value Service Estimator Component](https://github.com/llarsson/value-service-estimator) repo for how to use the code. As with the Caching interceptor, you may want to use the reverse proxy that [our modified Protobuf compiler](https://github.com/llarsson/protobuf) gives you, but (again!) should not have to.

