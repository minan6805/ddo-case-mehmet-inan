"""Validate the API contract with the pinned container-only validator."""
import json
import sys

from openapi_spec_validator import validate_spec

with open(sys.argv[1], encoding='utf-8') as source:
    validate_spec(json.load(source))
print('OpenAPI 3.0.3 PASS')
