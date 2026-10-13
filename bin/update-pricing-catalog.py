#!/usr/bin/env python3
import argparse
import concurrent.futures
import datetime
import json
import math
import hashlib
import pathlib
import urllib.request

parser = argparse.ArgumentParser(description='Import public OpenRouter endpoint prices; review and rebuild to publish.')
parser.add_argument('--sources', type=pathlib.Path, required=True)
parser.add_argument('--download', action='store_true', help='Download public metadata once, using five workers; never performs inference')
parser.add_argument('--channels', type=pathlib.Path, help='Enabled channel model/mapping export for bounded text discovery')
parser.add_argument('--output', type=pathlib.Path, default=pathlib.Path(__file__).resolve().parents[1] / 'model/pricing_catalog.json')
args = parser.parse_args()


if args.download:
    if args.channels is None:
        parser.error('--download requires --channels')
    args.sources.mkdir(parents=True, exist_ok=True)
    def download(source, model_id=None):
        record = {'source':source, 'fetched_at':datetime.datetime.now(datetime.timezone.utc).isoformat()}
        if model_id is not None:
            record['model_id'] = model_id
        try:
            with urllib.request.urlopen(source, timeout=30) as response:
                record.update(http_status=response.status, body=json.load(response))
        except Exception as error:
            record.update(http_status=0, body={}, error=type(error).__name__)
        return record
    chat = download('https://openrouter.ai/api/v1/models')
    images = download('https://openrouter.ai/api/v1/images/models')
    if chat['http_status'] != 200 or images['http_status'] != 200:
        raise ValueError('Public model discovery failed; previous source remains unchanged')
    by_id = {model['id']:model for model in chat['body']['data']}
    needed = set()
    for channel in json.loads(args.channels.read_text()):
        if channel['status'] != 1 or channel['type'] != 20:
            continue
        mapping = channel.get('model_mapping') or {}
        if isinstance(mapping, str):
            mapping = json.loads(mapping)
        for model_id in channel['models'].split(','):
            seen = set()
            while model_id in mapping and model_id not in seen:
                seen.add(model_id)
                model_id = mapping[model_id]
            if model_id not in seen and model_id in by_id:
                needed.add(model_id)
    def text_endpoint(model_id):
        return download('https://openrouter.ai'+by_id[model_id]['links']['details'], model_id)
    def image_endpoint(model):
        return download('https://openrouter.ai'+model['endpoints'], model['id'])
    with concurrent.futures.ThreadPoolExecutor(max_workers=5) as executor:
        text_records = list(executor.map(text_endpoint, sorted(needed)))
        image_records = list(executor.map(image_endpoint, images['body']['data']))
    if any(record['http_status'] != 200 for record in text_records+image_records):
        raise ValueError('Endpoint discovery incomplete; previous source remains unchanged')
    for name, records in [('chat',chat), ('images',images), ('text-endpoints',text_records), ('image-endpoints',image_records)]:
        (args.sources / (name+'.json')).write_text(json.dumps(records, indent=2)+'\n')


def amount(value):
    if value is None:
        return None
    result = float(value)
    if not math.isfinite(result) or result < 0:
        raise ValueError('Invalid published price')
    return result

def price(component, unit, value):
    return {'component': component, 'unit': unit, 'amount_usd': amount(value)}

models = {}
for record in json.loads((args.sources / 'image-endpoints.json').read_text()):
    if record['http_status'] != 200:
        raise ValueError('Incomplete image endpoint download')
    model_id = record['model_id']
    offers = []
    published_tariff_source = None
    endpoints = record['body'].get('endpoints', [])
    if not endpoints and model_id == 'meta/muse-image':
        endpoints = [{'pricing': [{'billable': 'output_image', 'unit': 'image', 'cost_usd': .01}]}]
        published_tariff_source = 'https://openrouter.ai/meta/muse-image'
    for index, endpoint in enumerate(endpoints):
        rates = endpoint.get('pricing', [])
        if not rates and endpoint.get('provider_tag') == 'krea':
            rate = {'krea/krea-2-large': .06, 'krea/krea-2-medium': .03, 'krea/krea-2-medium-turbo': .015}.get(model_id)
            if rate is not None:
                rates = [{'billable': 'output_image', 'unit': 'image', 'cost_usd': rate}]
                published_tariff_source = 'https://openrouter.ai/'+model_id
        variants = {row.get('variant', '') for row in rates} or {''}
        if variants != {''}:
            variants.discard('')
        capabilities = endpoint.get('supported_parameters', {})
        for variant in sorted(variants):
            if variant:
                known_values = [str(value).lower() for capability in capabilities.values() for value in capability.get('values', [])]
                if variant.lower() not in known_values:
                    continue
            prices = []
            for row in rates:
                if row.get('variant', '') not in ('', variant):
                    continue
                component = {'output_image':'image_output', 'input_image':'image_input', 'input_text':'input', 'input_reference':'reference_input', 'input_font':'font_input'}.get(row['billable'])
                unit = {'token':'million_tokens', 'image':'image', 'megapixel':'megapixel', 'request':'call'}.get(row['unit'])
                if component is None or unit is None:
                    raise ValueError('Unknown image billing basis')
                value = amount(row.get('cost_usd'))
                if value is not None and row['unit'] == 'token':
                    value *= 1000000
                prices.append(price(component, unit, value))
            if len({row['component'] for row in prices}) != len(prices):
                continue
            offers.append({'id':'image:'+str(endpoint.get('provider_tag', 'published'))+':'+variant+':'+hashlib.sha256(json.dumps(prices,sort_keys=True).encode()).hexdigest()[:16], 'operation':'image_generation', 'mode':'sync', 'main_component':'image_output', 'prices':prices})
    models[model_id] = {'source':record['source'], 'fetched_at':record['fetched_at'], 'offers':offers}
    if published_tariff_source:
        models[model_id]['published_tariff_source'] = published_tariff_source

endpoint_file = args.sources / 'text-endpoints.json'
if endpoint_file.exists():
    for record in json.loads(endpoint_file.read_text()):
        if record['http_status'] != 200:
            raise ValueError('Incomplete text endpoint download')
        offers = []
        body = record['body'].get('data', record['body'])
        for index, endpoint in enumerate(body.get('endpoints', [])):
            published = endpoint.get('pricing', {})
            base = {key:value for key,value in published.items() if key != 'overrides'}
            variants = [base]
            valid = True
            context_length = endpoint.get('context_length')
            overrides = published.get('overrides') or []
            if any(override.get('min_prompt_tokens') == 0 for override in overrides):
                variants = []
            for override in overrides:
                threshold = override.get('min_prompt_tokens')
                permitted = {'min_prompt_tokens','prompt','completion','input_cache_read','input_cache_write','input_cache_write_1h','request','image'}
                if not isinstance(threshold, int) or threshold < 0 or set(override)-permitted:
                    valid = False
                    break
                if isinstance(context_length, int) and threshold > context_length:
                    continue
                variants.append(base | {key:value for key,value in override.items() if key != 'min_prompt_tokens'})
            if not valid:
                continue
            for variant_index, pricing in enumerate(variants):
                prices = []
                for key, component in {'prompt':'input','completion':'output','input_cache_read':'cache_read','input_cache_write':'cache_write','input_cache_write_1h':'cache_write_1h'}.items():
                    if key in pricing:
                        value = amount(pricing[key])
                        prices.append(price(component, 'million_tokens', None if value is None else value * 1000000))
                if 'request' in pricing:
                    prices.append(price('base', 'call', pricing['request']))
                if 'image' in pricing:
                    prices.append(price('image_input', 'image', pricing['image']))
                offers.append({'id':'text:'+str(endpoint.get('tag', endpoint.get('provider_name','published')))+':'+hashlib.sha256(json.dumps(prices,sort_keys=True).encode()).hexdigest()[:16], 'operation':'text', 'mode':'batch' if record['model_id'].endswith(':batch') else 'sync', 'main_component':'output', 'prices':prices})
        models.setdefault(record['model_id'], {'source':record['source'], 'fetched_at':record['fetched_at'], 'offers':[]})['offers'].extend(offers)

if len(models) > 1024 or any(len(model['offers']) > 256 for model in models.values()):
    raise ValueError('Public price source exceeds bounded snapshot capacity')
snapshot = {'version':1, 'models':models}
temporary = args.output.with_suffix('.json.tmp')
temporary.write_text(json.dumps(snapshot, ensure_ascii=False, indent=2) + '\n')
temporary.replace(args.output)
print(f'Imported {len(models)} public model records into {args.output}; review, build and restart to publish')
