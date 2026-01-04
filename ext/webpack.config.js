
const webpack = require('webpack'),
    path = require('path'),
    //fileSystem = require('fs'),
    CleanWebpackPlugin = require('clean-webpack-plugin').CleanWebpackPlugin,
    MiniCssExtractPlugin = require('mini-css-extract-plugin'),
    CopyWebpackPlugin = require('copy-webpack-plugin'),
    HtmlWebpackPlugin = require('html-webpack-plugin'),
    TerserPlugin = require('terser-webpack-plugin');


function srcDir(f) {
    return path.join(__dirname, 'src', f);
}

function dstDir(f) {
    return path.join(__dirname, 'dist', f);
}

const MODE = process.env.NODE_ENV == 'production' ? 'production' : 'development';

const rules = [
    {
        test: /\.css$/,
        use: [MiniCssExtractPlugin.loader,
            'css-loader'],
        exclude: /node_modules/
    },
    {
        test: /\.(jpe?g|svg|png|gif|ico|eot|ttf|woff2?)(\?v=\d+\.\d+\.\d+)?$/i,
        type: 'asset/resource',
    },
    {
        test: /\.html$/,
        loader: 'html-loader',
        options: {
            sources: false
        },
        exclude: /node_modules/
    },
    {
        test: /\.ts?$/,
        use: 'ts-loader',
        exclude: /node_modules/,
    },
];

const resolve = {
    extensions: ['.ts', '.js'],
};

const optimization = {
    minimize: MODE == 'production',
    minimizer: [
        new TerserPlugin()
    ]
};


const addon = {
    mode: MODE,
    entry: {
        background: [srcDir('/background/background.ts')],
        content: [srcDir('/content/content.ts')],
        popup: [srcDir('/popup/popup.ts'), srcDir('../../server/static/css/style.css')],
    },
    output: {
        path: dstDir('/'),
        publicPath: '',
        filename: '[name].js'
    },
    optimization: optimization,
    module: {rules: rules},
    resolve: resolve,
    plugins: [
        new CleanWebpackPlugin({
            cleanStaleWebpackAssets: false,
        }),
        new MiniCssExtractPlugin({
            filename: '[name].css',
        }),
        new CopyWebpackPlugin({
            patterns: [
                // create chrome manifest.json
                {
                    from: 'src/manifest.json',
                    transform: function (content, path) {
						// generates the manifest file using the package.json informations
						content = JSON.parse(content.toString());
                        content['version'] = process.env.npm_package_version;
                        content['background']['service_worker'] = 'background.js';
						return Buffer.from(JSON.stringify(content));
					},
                    to: 'manifest.json'
                },
                // create ff manifest.json
                {
                    from: 'src/manifest.json',
                    transform: function (content, path) {
						// generates the manifest file using the package.json informations
						content = JSON.parse(content.toString());
                        content['version'] = process.env.npm_package_version;
                        content['background']['scripts'] = ['background.js'];
                        let ff_settings = {
                            "id": "{f0bda7ce-0cda-42dc-9ea8-126b20fed280}",
                            "strict_min_version": "110.0",
                            "data_collection_permissions": {
                                "required": ["browsingActivity", "websiteContent"],
                            },
                        };
						content['browser_specific_settings'] = {
							"gecko": ff_settings,
							"gecko_android": ff_settings,
						};
						return Buffer.from(JSON.stringify(content));
					},
                    to: 'manifest_ff.json'
                },
                {
                    from: 'assets/icon128.png',
                    to: 'assets/icons'
                },
                {
                    from: 'assets/logo.png',
                    to: 'assets'
                },
            ]
        }),
        new HtmlWebpackPlugin({
            template: srcDir('/popup/popup.html'),
            filename: 'popup.html',
            inject: false
        }),
        new HtmlWebpackPlugin({
            template: srcDir('/options/options.html'),
            filename: 'options.html'
        })
    ],
    devtool: 'cheap-module-source-map'
};


module.exports = [
    addon
];
