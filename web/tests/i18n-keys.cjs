/* eslint-disable @typescript-eslint/no-require-imports */
/**
 * Reconciles translation keys used in source against the locale JSON files.
 *
 * Resolves `useTranslations('ns')` bindings per lexical scope, then walks every
 * call on those bindings to build fully-qualified keys. Callers that cannot be
 * resolved statically (dynamic key, dynamic namespace, translator received as a
 * parameter) are reported separately instead of being silently dropped.
 */
const fs = require('node:fs');
const path = require('node:path');
const ts = require('typescript');

const webRoot = path.join(__dirname, '..');
const srcRoot = path.join(webRoot, 'src');
const localeDir = path.join(webRoot, 'src', 'locales');
const LOCALE_FILES = ['zh_hans.json', 'zh_hant.json', 'en.json'];

const TRANSLATOR_FACTORIES = new Set(['useTranslations', 'getTranslations']);
const TRANSLATOR_METHODS = new Set(['rich', 'markup', 'raw', 'has']);
const TRANSLATOR_PARAM_TYPE = /ReturnType<\s*typeof\s+(useTranslations|getTranslations)\s*>/;

/** Namespace sentinel: translator exists but its namespace is not statically known. */
const NS_UNKNOWN = Symbol('unknown-namespace');

function readJson(filePath) {
    return JSON.parse(fs.readFileSync(filePath, 'utf8'));
}

/** Leaf keys (string values) and container keys (object values), tracked apart. */
function collectKeys(value, prefix = '', leaves = new Set(), containers = new Set()) {
    for (const [key, nested] of Object.entries(value)) {
        const next = prefix ? `${prefix}.${key}` : key;
        if (nested && typeof nested === 'object' && !Array.isArray(nested)) {
            containers.add(next);
            collectKeys(nested, next, leaves, containers);
        } else {
            leaves.add(next);
        }
    }
    return { leaves, containers };
}

/** Backwards-compatible helper: leaf keys only. */
function flattenLeafKeys(value, prefix = '') {
    return collectKeys(value, prefix).leaves;
}

function collectSourceFiles(dir, files = []) {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
        const fullPath = path.join(dir, entry.name);
        if (entry.isDirectory()) {
            if (entry.name === 'node_modules') continue;
            collectSourceFiles(fullPath, files);
            continue;
        }
        if (/\.(ts|tsx)$/.test(entry.name) && !entry.name.endsWith('.d.ts')) {
            files.push(fullPath);
        }
    }
    return files;
}

function staticText(node) {
    if (!node) return null;
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
    return null;
}

/**
 * If `init` is a translator factory call, return its namespace:
 * a string, '' for the root namespace, or NS_UNKNOWN when computed dynamically.
 */
function namespaceFromInitializer(init) {
    let node = init;
    if (node && ts.isAwaitExpression(node)) node = node.expression;
    if (!node || !ts.isCallExpression(node)) return undefined;
    if (!ts.isIdentifier(node.expression)) return undefined;
    if (!TRANSLATOR_FACTORIES.has(node.expression.text)) return undefined;

    const arg = node.arguments[0];
    if (!arg) return '';
    const text = staticText(arg);
    return text === null ? NS_UNKNOWN : text;
}

function qualify(namespace, key) {
    return namespace ? `${namespace}.${key}` : key;
}

function scanFile(filePath) {
    const source = fs.readFileSync(filePath, 'utf8');
    const sourceFile = ts.createSourceFile(
        filePath,
        source,
        ts.ScriptTarget.Latest,
        true,
        filePath.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
    );
    const relPath = path.relative(webRoot, filePath).replace(/\\/g, '/');
    const lineOf = (node) => sourceFile.getLineAndCharacterOfPosition(node.getStart(sourceFile)).line + 1;

    const used = [];
    const dynamic = [];
    const unresolved = [];

    /** Every identifier bound by a binding pattern, however nested. */
    function bindingNames(name, out = []) {
        if (ts.isIdentifier(name)) {
            out.push(name.text);
        } else if (ts.isObjectBindingPattern(name) || ts.isArrayBindingPattern(name)) {
            for (const element of name.elements) {
                if (ts.isBindingElement(element)) bindingNames(element.name, out);
            }
        }
        return out;
    }

    /**
     * Declarations introduced directly by `node`'s own statements/parameters.
     * Non-translator bindings are recorded as shadows (namespace `undefined`) so an
     * inner `arr.map((t) => t(x))` cannot be misread as a call on an outer `t`.
     */
    function declarationsIn(node) {
        const found = [];

        if (ts.isSourceFile(node) || ts.isBlock(node) || ts.isModuleBlock(node) || ts.isCaseClause(node)) {
            for (const statement of node.statements ?? []) {
                if (!ts.isVariableStatement(statement)) continue;
                for (const decl of statement.declarationList.declarations) {
                    const ns = ts.isIdentifier(decl.name) ? namespaceFromInitializer(decl.initializer) : undefined;
                    if (ns !== undefined) {
                        found.push([decl.name.text, ns]);
                    } else {
                        for (const bound of bindingNames(decl.name)) found.push([bound, undefined]);
                    }
                }
            }
        }

        for (const param of node.parameters ?? []) {
            // A translator arriving as a parameter, either directly typed
            // (`t: ReturnType<typeof useTranslations>`) or via an inline props type
            // (`{ t }: { t: ReturnType<typeof useTranslations> }`). Its namespace is
            // fixed by the caller, so keys here cannot be qualified statically.
            const translatorProps = new Set();
            if (param.type && ts.isTypeLiteralNode(param.type)) {
                for (const member of param.type.members) {
                    if (!ts.isPropertySignature(member) || !member.type) continue;
                    if (!TRANSLATOR_PARAM_TYPE.test(member.type.getText(sourceFile))) continue;
                    if (member.name && ts.isIdentifier(member.name)) translatorProps.add(member.name.text);
                }
            }
            const wholeParamIsTranslator =
                !!param.type && !ts.isTypeLiteralNode(param.type) && TRANSLATOR_PARAM_TYPE.test(param.type.getText(sourceFile));

            for (const bound of bindingNames(param.name)) {
                const isTranslator = wholeParamIsTranslator || translatorProps.has(bound);
                found.push([bound, isTranslator ? NS_UNKNOWN : undefined]);
            }
        }

        return found;
    }

    /** Resolve the translator binding behind a call target, if any. */
    function translatorFor(expression, scope) {
        // `scope.get() === undefined` means the name is bound to something that is
        // not a translator (a shadow); only defined namespaces count.
        if (ts.isIdentifier(expression)) {
            const ns = scope.get(expression.text);
            return ns === undefined ? null : { name: expression.text, ns };
        }
        if (
            ts.isPropertyAccessExpression(expression) &&
            ts.isIdentifier(expression.expression) &&
            ts.isIdentifier(expression.name) &&
            TRANSLATOR_METHODS.has(expression.name.text)
        ) {
            const ns = scope.get(expression.expression.text);
            return ns === undefined ? null : { name: expression.expression.text, ns };
        }
        return null;
    }

    function visit(node, scope) {
        const declared = declarationsIn(node);
        const nextScope = declared.length > 0 ? new Map([...scope, ...declared]) : scope;

        if (ts.isCallExpression(node)) {
            const translator = translatorFor(node.expression, nextScope);
            if (translator) {
                const keyArg = node.arguments[0];
                const key = staticText(keyArg);
                const location = { file: relPath, line: lineOf(node), translator: translator.name };

                if (key === null) {
                    dynamic.push({
                        ...location,
                        // The namespace is known even when the key is not; record it so
                        // dynamic families can still be checked by prefix.
                        namespace: translator.ns === NS_UNKNOWN ? null : translator.ns,
                        text: keyArg ? keyArg.getText(sourceFile) : '<no argument>',
                    });
                } else if (translator.ns === NS_UNKNOWN) {
                    unresolved.push({ ...location, key });
                } else {
                    used.push({ ...location, key: qualify(translator.ns, key) });
                }
            }
        }

        ts.forEachChild(node, (child) => visit(child, nextScope));
    }

    visit(sourceFile, new Map());
    return { used, dynamic, unresolved };
}

function scanSources() {
    const used = [];
    const dynamic = [];
    const unresolved = [];

    for (const filePath of collectSourceFiles(srcRoot)) {
        const result = scanFile(filePath);
        used.push(...result.used);
        dynamic.push(...result.dynamic);
        unresolved.push(...result.unresolved);
    }

    return { used, dynamic, unresolved };
}

function loadLocales() {
    return LOCALE_FILES.map((fileName) => {
        const { leaves, containers } = collectKeys(readJson(path.join(localeDir, fileName)));
        return { fileName, keys: leaves, containers };
    });
}

/**
 * Splits problems into two kinds:
 *  - `missing`:   the key resolves to nothing in a locale file.
 *  - `notLeaf`:   the key resolves to an object, so next-intl cannot render it
 *                 (the call site wants `<key>.title` or similar).
 *
 * @returns {{ missing: Array, notLeaf: Array, dynamic: Array, unresolved: Array,
 *             usedKeys: Set<string>, usagesByKey: Map, locales: Array }}
 */
function analyze() {
    const { used, dynamic, unresolved } = scanSources();
    const locales = loadLocales();

    const usagesByKey = new Map();
    for (const usage of used) {
        if (!usagesByKey.has(usage.key)) usagesByKey.set(usage.key, []);
        usagesByKey.get(usage.key).push(usage);
    }

    const missing = [];
    const notLeaf = [];
    for (const { fileName, keys, containers } of locales) {
        for (const [key, usages] of usagesByKey) {
            if (keys.has(key)) continue;
            if (containers.has(key)) {
                notLeaf.push({ locale: fileName, key, usages });
            } else {
                missing.push({ locale: fileName, key, usages });
            }
        }
    }

    return { missing, notLeaf, dynamic, unresolved, usedKeys: new Set(usagesByKey.keys()), usagesByKey, locales };
}

module.exports = { analyze, scanSources, loadLocales, collectKeys, flattenLeafKeys, LOCALE_FILES, localeDir, webRoot };
