# Backendify

Hey there, and welcome to the challenge!

At Backendify, our goal is to make our customer's lives easier. No one has to deal with the complexity of having multiple providers for the same kind of data!

We have several services that return company data when given country iso code and a company id.

You will build a proxy service that provides the same API for all of them.

## Deliverable

Your solution will be deployed to production automatically. Therefore, you will need to conform to the following requirements:

1. Your solution should be a docker container. If you don't know how to do it, don't worry, the release team already did all the heavy lifting. You will only need to modify the provided Dockerfile template, and the CI will build the image for you and release it into the correct registry. The details are in the .gitlab-ci.yml file.

2. Your solution should listen and accept HTTP requests on the TCP port 9000. That is the port the security team dedicated to your application and opened on the firewalls.

3. It should get the list of backends from the command line at startup. The format is a list of "iso-code=backend-address" mappings, for example: `./your-solution ru=http://localhost:9001 us=http://localhost:9002`. Backendify has only one backend per country as of now, so the ISO country codes are guaranteed to be unique.

You can freely modify any file in the repository to suit your needs, as long as you honor the above requirement.

## Solution API design

The API customers are expecting is quite simple.

Your application must implement just two endpoints:

1. `GET /status`.  This API endpoint must return an HTTP 200/OK when your solution is ready to accept requests. The load balancer will use that endpoint to monitor your solution in production.

2. `GET /company?id=XX&country_iso=YY`. This API endpoint receives the parameters id and country_iso. `id` can be a string without any particular limitation. `country_iso` will be a two-letter country code to select the backend according to the application configuration. Your solution must query the backend in a proper country and return:
    - An HTTP 200/OK reply when the company with the requested id exists on the corresponding backend. The body should be a JSON object with the company data returned by a backend.
    - An HTTP 404/Not Found reply if the company with the requested id does not exist on the corresponding backend.

Your solution should always reply with the following JSON object to the customer:

```json
{
  "id": "string, the company id requested by a customer",
  "name": "string, the company name, as returned by a backend",
  "active": "boolean, indicating if the company is still active according to the active_until date",
  "active_until": "RFC 3339 UTC date-time expressed as a string, optional."
}
```

## Backend providers API description

As of now, there are only two backend variants, V1 and V2. Backendify, in compliance with the industry's best practices, is in a state of transition between the two backends, so your solution must support both.

Both backends will answer HTTP GET requests on the `/companies/<id>` path, where `id` is the arbitrary string. However, their replies are slightly different:

1. V1 backend will return the JSON object of the following format:

```json
{
  "cn": "string, the company name.",
  "created_on": "RFC3339 UTC datetime expressed as a string.",
  "closed_on": "RFC3339 UTC datetime expressed as a string, optional.",
}
```

2. V1 backend reply will have a Content-Type of an `application/x-company-v1`.

3. V2 backend will return the JSON object of the following format:
```json
{
  "company_name": "string, the company name.",
  "tin": "string, tax identification number",
  "dissolved_on": "RFC3339 UTC datetime expressed as a string, optional.",
}
```

4. V2 backend reply will have a different Content-Type of an `application/x-company-v2`.

## Running in Production

This repository has a continuous delivery setup. Every time you push code to the repo, it will deploy your solution to the production environment as a single instance. The orchestrator will give it at least 1 CPU, 128MB of RAM, and a volatile 1G drive located at `/tmp`.

When your solution reports that it is ready to serve requests (with the `/status` endpoint described above), the load balancer will unleash customer traffic to it.

Things to consider when dealing with the backends:

1. They are located in distant areas. They are not reliable. They can time out, return errors, or throttle you. Some of them are in dire need of an upgrade and are not particularly fast.

2. Your solution has an SLA when replying to a customer: 95% of requests should reply within 1 second. If your application does not answer within an SLA, the customers will consider this an error, abort the request, and retry aggresively a number of times.

3. To account for unreliable backends and still stay within an SLA, your solution might use caching. You can cache any correct reply from the backend for 24 hours as data is really slow to change.

## Accessing production to debug

You can directly access the logs in the canary deployment.

In production, there is no direct access to logs due to the extremely high load. At thousands of requests per second, the engineering team decided to provide a StatD-compatible metrics server instead, and your solution can use up to five metrics. Check the FAQ for more information.

## Finally

Remember: this is not an exam. There are no prescriptive answers, no right or wrong ways to do things, no checkboxes to hit, and no time limit. All that matters is that your solution solves the business problem, and the Backendify customers are happy with it.

Good luck! And have fun :D
