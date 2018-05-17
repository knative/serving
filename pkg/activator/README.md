# The Activator Network Configurations

## Traffic flow overview
An ingress object is created per Route to direct external traffic to reach desired revisions. This ingress
object has annotations "istio" so Istio is the ingress controller which fulfills the ingress. The ingress
uses domain based routing to map the requests to the placeholder service. The placeholder service is then
mapped to Istio routerules which controll traffic across revisions and the activator service.

The picture below shows traffic flow for a route "abc-route".

![traffic](images/routeTraffic.png)

## Istio Route Rules Configurations

Elafros Route objects control traffic split via Istio route rules. When a revision is in Reserve state
due to inactivity, instead of letting the revision get traffic assignment directly, Route defines route
rules such that the activator gets the portion of traffic for the revision.

### Route rules when all revisions are active

When all revisions are active, the activator service does not get any traffic.
Below is an example where the route (my-service) has two traffic targets, Revision a and Revision b, and
both revisions are active. Note activator does not appear.

![active revision](images/activator_activeRevision.png)

### Route rules when one revision is in Reserve state

When one revision is in Reserve state, the activator services gets its traffic assignment.
Below is an example where Revision a is active and Revision b is in Reserve. In this case, the Activator
gets traffic assignment for Revision b. Upon receiving requests, Activator activates Revision b and
forwards requests to Revision b after it is ready. After the revision is activated and ready to serve
traffic, the revision gets assigned portion of traffic directly.

![reserve revision](images/activator_reserveRevision.png)

### Route rules when multiple revisions are in Reserve state

When there are two or more revisions are in Reserve state, the Activator service gets traffic for all
Reserve revisions. Among Reserve revisions, Activator activates the revision with the largest traffic
weight, and forwards traffic to it. There is room for improvement for this behavior ([#882](https://github.com/elafros/elafros/issues/882)).
After the revision is activated and ready to serve traffic, activator gets the protion of traffic
for all the rest Reserve revisions.