#!/usr/local/bin/dumb-init /bin/bash
set -euo pipefail
[ 'true' = "${DEBUG:-}" ] && set -x

base=$PWD

VERSION=$(cat "$base"/version/number)
if [ -z "${VERSION:-}" ]; then
  echo "missing version number"
  exit 1
fi

TILE_FILE=`cd artifacts; ls *-${VERSION}.pivotal`
if [ -z "${TILE_FILE}" ]; then
	echo "No files matching artifacts/*.pivotal"
	ls -lR artifacts
	exit 1
fi

STEMCELL_FILE=`cd stemcell; ls *bosh-stemcell-*.tgz`
if [ -z "${STEMCELL_FILE}" ]; then
	echo "No files matching stemcell/*.tgz"
	ls -lR stemcell
	exit 1
fi

PRODUCT="$(cat $base/mongodb-on-demand-release/tile/tile.yml | grep '^name' | cut -d' ' -f 2)"

om="om -t $PCF_URL -u $PCF_USERNAME -p $PCF_PASSWORD -k"

${om} upload-product --product "artifacts/$TILE_FILE"
${om} upload-stemcell --stemcell "stemcell/$STEMCELL_FILE"
${om} available-products
${om} stage-product --product-name "$PRODUCT" --product-version "$VERSION"

echo "$PRODUCT_PROPERTIES" > properties.yml
echo "$PRODUCT_NETWORK_AZS" > network-azs.yml

properties_config=$(ruby -ryaml -rjson -e 'puts JSON.pretty_generate(YAML.load(ARGF))' < properties.yml)
properties_config=$(echo "$properties_config" | jq 'delpaths([path(.[][] | select(. == null))]) | delpaths([path(.[][] | select(. == ""))]) | delpaths([path(.[] | select(. == {}))])')

network_config=$(ruby -ryaml -rjson -e 'puts JSON.pretty_generate(YAML.load(ARGF))' < network-azs.yml)

${om} configure-product --product-name "$PRODUCT" --product-network "$network_config" --product-properties "$properties_config"

STAGED=$(${om} curl --path /api/v0/staged/products)
RESULT=$(echo "$STAGED" | jq --arg product_name "$PRODUCT" 'map(select(.type == $product_name)) | .[].guid')
DATA=$(echo '{"deploy_products": []}' | jq ".deploy_products += [$RESULT]")

${om} curl --path /api/v0/installations --request POST --data "$DATA"
${om} apply-changes --skip-deploy-products="true"
${om} delete-unused-products
