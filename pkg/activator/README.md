# The Activator Network Configurations

## Traffic flow overview
An ingress object is created per Route to direct external traffic to reach cluster services. This ingress
object has annotations "istio" so Istio is the ingress controller which fulfills the ingress. The ingress
uses domain based routing to point to a placeholder service. The placeholder service is then mapped to 
Istio routerules which controll traffic across revisions and the activator service.

The picture below shows traffic flow for a route "abc-route".

![traffic](images/routeTraffic.png)

## Istio Route Rules Configurations

Elafros Route objects control traffic split via Istio route rules. When a revision is in Reserve state
due to inactivity, instead of letting the revision get traffic assignment directly, Route defines route
rules such that the activator gets the portion of traffic for the revision.

### Route rules when at most one revision is in Reserve state
Below is an example where the route (my-service) has two traffic targets, Revision a and Revision b, and
both revisions are active.

![active revision](images/activator_activeRevision.png)

When Revision b is in Reserve state, the route would have Revision a and the Activator as traffic targets.
Upon receiving requests, Activator activates Revision b and forwards requests to Revision b after it is
ready.

![reserve revision](images/activator_reserveRevision.png)

### Route rules when multiple revisions are in Reserve state
When there are two or more revisions are in Reserve state, the Activator service gets traffic for all
Reserve revisions. Among Reserve revisions, Activator activates the revision with the largest traffic
weight, and forwards traffic to it. There is room for improvement for this behavior ([#882](https://github.com/elafros/elafros/issues/882)).