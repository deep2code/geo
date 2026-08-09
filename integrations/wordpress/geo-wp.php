<?php
/**
 * Plugin Name: GEO Content AI Optimizer (mu-plugin)
 * Plugin URI:  https://github.com/example/my-geo
 * Description: 将 WordPress 文章内容通过 GEO REST API 进行生成式引擎可见度（GEO）评分与优化建议；提供 wp-admin 编辑器集成、REST 转发路由、[geo_score] 短代码及设置页。
 * Version:     1.0.0
 * Author:      GEO Team
 * Author URI:  https://example.com
 * License:     MIT
 * Text Domain: geo-wp
 * Domain Path: /languages
 */

if (!defined('ABSPATH')) {
    exit;
}

define('GEO_WP_VERSION', '1.0.0');
define('GEO_WP_OPTION_ENDPOINT', 'geo_endpoint');
define('GEO_WP_OPTION_TOKEN', 'geo_token');
define('GEO_WP_DEFAULT_OK_THRESHOLD', 60);

function geo_wp_get_endpoint()
{
    $ep = get_option(GEO_WP_OPTION_ENDPOINT, '');
    if ($ep === '' && defined('GEO_ENDPOINT')) {
        $ep = (string) GEO_ENDPOINT;
    }
    return rtrim(trim($ep), '/');
}

function geo_wp_get_token()
{
    $tk = get_option(GEO_WP_OPTION_TOKEN, '');
    if ($tk === '' && defined('GEO_TOKEN')) {
        $tk = (string) GEO_TOKEN;
    }
    return $tk;
}

function geo_wp_request($method, $path, $body = null)
{
    $endpoint = geo_wp_get_endpoint();
    if ($endpoint === '') {
        return new WP_Error('geo_missing_endpoint', __('GEO_ENDPOINT 未配置，请先在 设置 → GEO 中填写。', 'geo-wp'));
    }
    $url = $endpoint . $path;
    $args = array(
        'method'  => strtoupper($method),
        'timeout' => 30,
        'headers' => array(
            'Content-Type' => 'application/json',
            'Accept'       => 'application/json',
        ),
    );
    $token = geo_wp_get_token();
    if ($token !== '') {
        $args['headers']['Authorization'] = 'Bearer ' . $token;
    }
    if ($body !== null) {
        $args['body'] = wp_json_encode($body);
    }
    $resp = wp_remote_request($url, $args);
    if (is_wp_error($resp)) {
        return $resp;
    }
    $code = (int) wp_remote_retrieve_response_code($resp);
    $raw  = wp_remote_retrieve_body($resp);
    $data = json_decode($raw, true);
    if ($code < 200 || $code >= 300) {
        $msg = is_array($data) && isset($data['error']) ? $data['error'] : sprintf(__('GEO 服务返回 %d', 'geo-wp'), $code);
        return new WP_Error('geo_http_' . $code, $msg, array('status' => $code, 'body' => $data));
    }
    return $data;
}

function geo_wp_extract_post_html($post)
{
    if (is_numeric($post)) {
        $post = get_post($post);
    }
    if (!$post instanceof WP_Post) {
        return '';
    }
    $content = apply_filters('the_content', $post->post_content);
    $title   = get_the_title($post);
    $html    = '<h1>' . esc_html($title) . '</h1>' . "\n" . $content;
    return $html;
}

function geo_wp_check_post($post)
{
    $html   = geo_wp_extract_post_html($post);
    $postId = is_numeric($post) ? (int) $post : $post->ID;
    $body = array(
        'html'   => $html,
        'url'    => (string) get_permalink($postId),
        'title'  => (string) get_the_title($postId),
        'domain' => (string) wp_parse_url(home_url(), PHP_URL_HOST),
    );
    return geo_wp_request('POST', '/api/v1/cms/check', $body);
}

add_action('rest_api_init', 'geo_wp_register_rest_routes');

function geo_wp_register_rest_routes()
{
    register_rest_route('geo/v1', '/info', array(
        'methods'             => WP_REST_Server::READABLE,
        'callback'            => 'geo_wp_rest_info',
        'permission_callback' => function () {
            return current_user_can('edit_posts');
        },
    ));

    register_rest_route('geo/v1', '/check', array(
        'methods'             => WP_REST_Server::CREATABLE,
        'callback'            => 'geo_wp_rest_check',
        'permission_callback' => function () {
            return current_user_can('edit_posts');
        },
        'args'                => array(
            'post_id' => array(
                'type'     => 'integer',
                'required' => false,
            ),
            'html' => array(
                'type'     => 'string',
                'required' => false,
            ),
            'url' => array(
                'type'     => 'string',
                'required' => false,
            ),
            'title' => array(
                'type'     => 'string',
                'required' => false,
            ),
        ),
    ));

    register_rest_route('geo/v1', '/check/(?P<id>\d+)', array(
        'methods'             => WP_REST_Server::READABLE,
        'callback'            => 'geo_wp_rest_check_by_id',
        'permission_callback' => function () {
            return current_user_can('edit_posts');
        },
        'args'                => array(
            'id' => array(
                'type'     => 'integer',
                'required' => true,
            ),
        ),
    ));
}

function geo_wp_rest_info(WP_REST_Request $request)
{
    $endpoint = geo_wp_get_endpoint();
    if ($endpoint === '') {
        return new WP_REST_Response(array(
            'ok'      => false,
            'version' => GEO_WP_VERSION,
            'configured' => false,
            'error'   => __('GEO_ENDPOINT 未配置', 'geo-wp'),
        ), 200);
    }
    $remote = geo_wp_request('GET', '/api/v1/cms/info');
    if (is_wp_error($remote)) {
        return new WP_REST_Response(array(
            'ok'         => false,
            'version'    => GEO_WP_VERSION,
            'configured' => true,
            'endpoint'   => $endpoint,
            'error'      => $remote->get_error_message(),
        ), 200);
    }
    return new WP_REST_Response(array(
        'ok'         => true,
        'version'    => GEO_WP_VERSION,
        'configured' => true,
        'endpoint'   => $endpoint,
        'server'     => $remote,
    ), 200);
}

function geo_wp_rest_check(WP_REST_Request $request)
{
    $params = $request->get_json_params();
    if (empty($params)) {
        $params = $request->get_params();
    }
    $postId = isset($params['post_id']) ? (int) $params['post_id'] : 0;
    if ($postId > 0) {
        $result = geo_wp_check_post($postId);
        if (is_wp_error($result)) {
            return new WP_Error($result->get_error_code(), $result->get_error_message(), array('status' => 400));
        }
        return new WP_REST_Response($result, 200);
    }
    $html = isset($params['html']) ? (string) $params['html'] : '';
    if ($html === '') {
        return new WP_Error('geo_missing_html', __('请提供 post_id 或 html 参数', 'geo-wp'), array('status' => 400));
    }
    $body = array(
        'html'   => $html,
        'url'    => isset($params['url'])    ? (string) $params['url']    : '',
        'title'  => isset($params['title'])  ? (string) $params['title']  : '',
        'domain' => (string) wp_parse_url(home_url(), PHP_URL_HOST),
    );
    $result = geo_wp_request('POST', '/api/v1/cms/check', $body);
    if (is_wp_error($result)) {
        return new WP_Error($result->get_error_code(), $result->get_error_message(), array('status' => 400));
    }
    return new WP_REST_Response($result, 200);
}

function geo_wp_rest_check_by_id(WP_REST_Request $request)
{
    $postId = (int) $request->get_param('id');
    if ($postId <= 0 || !get_post($postId)) {
        return new WP_Error('geo_invalid_post', __('无效的文章 ID', 'geo-wp'), array('status' => 404));
    }
    $result = geo_wp_check_post($postId);
    if (is_wp_error($result)) {
        return new WP_Error($result->get_error_code(), $result->get_error_message(), array('status' => 400));
    }
    return new WP_REST_Response($result, 200);
}

add_action('transition_post_status', 'geo_wp_on_publish', 10, 3);

function geo_wp_on_publish($new_status, $old_status, $post)
{
    if ($new_status !== 'publish' || $old_status === 'publish') {
        return;
    }
    if (!in_array($post->post_type, array('post', 'page'), true)) {
        return;
    }
    $result = geo_wp_check_post($post->ID);
    if (is_wp_error($result)) {
        return;
    }
    $score     = isset($result['score'])       ? (float) $result['score']       : 0;
    $grade     = isset($result['grade'])       ? (string) $result['grade']      : 'F';
    $ok        = isset($result['ok'])          ? (bool) $result['ok']           : false;
    $sigs      = isset($result['signals'])     ? $result['signals']             : array();
    $suggests  = isset($result['suggestions']) ? $result['suggestions']         : array();
    update_post_meta($post->ID, '_geo_score',      $score);
    update_post_meta($post->ID, '_geo_grade',      $grade);
    update_post_meta($post->ID, '_geo_ok',         $ok ? '1' : '0');
    update_post_meta($post->ID, '_geo_signals',    $sigs);
    update_post_meta($post->ID, '_geo_suggestions', $suggests);
    update_post_meta($post->ID, '_geo_checked_at', current_time('mysql', true));
}

add_shortcode('geo_score', 'geo_wp_shortcode_score');

function geo_wp_shortcode_score($atts, $content = '')
{
    $atts = shortcode_atts(array(
        'id'     => 0,
        'format' => 'badge',
    ), $atts, 'geo_score');

    $postId = (int) $atts['id'] > 0 ? (int) $atts['id'] : get_the_ID();
    if (!$postId) {
        return '';
    }
    $score = get_post_meta($postId, '_geo_score', true);
    if ($score === '' || $score === false) {
        $result = geo_wp_check_post($postId);
        if (is_wp_error($result)) {
            return '';
        }
        $score = isset($result['score']) ? (float) $result['score'] : 0;
        $grade = isset($result['grade']) ? (string) $result['grade'] : 'F';
        $ok    = isset($result['ok'])    ? (bool) $result['ok']      : false;
    } else {
        $score = (float) $score;
        $grade = (string) get_post_meta($postId, '_geo_grade', true);
        $ok    = get_post_meta($postId, '_geo_ok', true) === '1';
    }
    $format = strtolower((string) $atts['format']);
    if ($format === 'number') {
        return esc_html(sprintf('%.1f', $score));
    }
    if ($format === 'grade') {
        return esc_html($grade);
    }
    $gradeColors = array(
        'A' => '#10B981',
        'B' => '#3B82F6',
        'C' => '#F59E0B',
        'D' => '#F97316',
        'F' => '#EF4444',
    );
    $color   = isset($gradeColors[$grade]) ? $gradeColors[$grade] : '#6B7280';
    $labelOk = $ok ? __('GEO 通过', 'geo-wp') : __('GEO 待优化', 'geo-wp');
    $html  = '<span class="geo-score-badge" style="display:inline-flex;align-items:center;gap:8px;padding:4px 12px;border-radius:9999px;background:#F3F4F6;font-size:13px;font-weight:500;">';
    $html .= '<span class="geo-score-grade" style="display:inline-flex;align-items:center;justify-content:center;min-width:24px;height:24px;border-radius:9999px;color:#fff;background:' . esc_attr($color) . ';">' . esc_html($grade) . '</span>';
    $html .= '<span class="geo-score-value" style="color:#111827;">' . esc_html(sprintf('%.1f', $score)) . '</span>';
    $html .= '<span class="geo-score-label" style="color:' . esc_attr($ok ? '#059669' : '#DC2626') . ';">' . esc_html($labelOk) . '</span>';
    $html .= '</span>';
    return apply_filters('geo_wp_score_badge_html', $html, $score, $grade, $ok, $postId);
}

add_filter('manage_posts_columns', 'geo_wp_add_posts_column');
add_filter('manage_pages_columns', 'geo_wp_add_posts_column');
add_action('manage_posts_custom_column', 'geo_wp_render_posts_column', 10, 2);
add_action('manage_pages_custom_column', 'geo_wp_render_posts_column', 10, 2);

function geo_wp_add_posts_column($columns)
{
    $columns['geo_score'] = __('GEO 评分', 'geo-wp');
    return $columns;
}

function geo_wp_render_posts_column($column, $postId)
{
    if ($column !== 'geo_score') {
        return;
    }
    echo geo_wp_shortcode_score(array('id' => $postId, 'format' => 'badge'));
}

add_action('admin_menu', 'geo_wp_add_settings_page');
add_action('admin_init', 'geo_wp_register_settings');

function geo_wp_add_settings_page()
{
    add_options_page(
        __('GEO 内容优化配置', 'geo-wp'),
        __('GEO', 'geo-wp'),
        'manage_options',
        'geo-wp-settings',
        'geo_wp_render_settings_page'
    );
}

function geo_wp_register_settings()
{
    register_setting('geo_wp_group', GEO_WP_OPTION_ENDPOINT, array(
        'type'              => 'string',
        'sanitize_callback' => function ($v) {
            return rtrim(trim((string) $v), '/');
        },
        'default'           => '',
    ));
    register_setting('geo_wp_group', GEO_WP_OPTION_TOKEN, array(
        'type'              => 'string',
        'sanitize_callback' => function ($v) {
            return trim((string) $v);
        },
        'default'           => '',
    ));
    add_settings_section(
        'geo_wp_section_api',
        __('GEO 服务连接', 'geo-wp'),
        'geo_wp_render_section_api_desc',
        'geo-wp-settings'
    );
    add_settings_field(
        GEO_WP_OPTION_ENDPOINT,
        __('GEO 服务地址 (GEO_ENDPOINT)', 'geo-wp'),
        'geo_wp_render_field_endpoint',
        'geo-wp-settings',
        'geo_wp_section_api'
    );
    add_settings_field(
        GEO_WP_OPTION_TOKEN,
        __('访问令牌 (GEO_TOKEN，可选)', 'geo-wp'),
        'geo_wp_render_field_token',
        'geo-wp-settings',
        'geo_wp_section_api'
    );
}

function geo_wp_render_section_api_desc()
{
    echo '<p>' . esc_html__('配置自建 GEO 服务的 REST API 地址，启用后可在发布时自动检测内容可见度评分。', 'geo-wp') . '</p>';
}

function geo_wp_render_field_endpoint()
{
    $value = esc_attr(get_option(GEO_WP_OPTION_ENDPOINT, ''));
    echo '<input type="url" name="' . esc_attr(GEO_WP_OPTION_ENDPOINT) . '" value="' . $value . '" class="regular-text code" placeholder="https://geo.example.com">';
    echo '<p class="description">' . esc_html__('也可通过 wp-config.php 定义常量 GEO_ENDPOINT 覆盖此设置。', 'geo-wp') . '</p>';
}

function geo_wp_render_field_token()
{
    $value = esc_attr(get_option(GEO_WP_OPTION_TOKEN, ''));
    echo '<input type="password" name="' . esc_attr(GEO_WP_OPTION_TOKEN) . '" value="' . $value . '" class="regular-text code" autocomplete="off" placeholder="Bearer token...">';
    echo '<p class="description">' . esc_html__('也可通过 wp-config.php 定义常量 GEO_TOKEN 覆盖此设置。', 'geo-wp') . '</p>';
}

function geo_wp_render_settings_page()
{
    if (!current_user_can('manage_options')) {
        return;
    }
    ?>
    <div class="wrap">
        <h1><?php esc_html_e('GEO 内容优化配置', 'geo-wp'); ?></h1>
        <form action="options.php" method="post">
            <?php
            settings_fields('geo_wp_group');
            do_settings_sections('geo-wp-settings');
            submit_button();
            ?>
        </form>
        <hr style="margin:30px 0;">
        <h2><?php esc_html_e('连接测试', 'geo-wp'); ?></h2>
        <p><?php esc_html_e('点击按钮检测与 GEO 服务的连通性（调用 GET /api/v1/cms/info）。', 'geo-wp'); ?></p>
        <button id="geo-wp-test-connection" class="button"><?php esc_html_e('测试连接', 'geo-wp'); ?></button>
        <pre id="geo-wp-test-result" style="margin-top:16px;padding:12px;background:#F9FAFB;border:1px solid #E5E7EB;display:none;white-space:pre-wrap;word-break:break-word;"></pre>
        <script type="text/javascript">
        (function () {
            var btn = document.getElementById('geo-wp-test-connection');
            var out = document.getElementById('geo-wp-test-result');
            btn.addEventListener('click', function () {
                btn.disabled = true;
                out.style.display = 'none';
                out.textContent = '';
                fetch('<?php echo esc_js(rest_url('geo/v1/info')); ?>', {
                    method: 'GET',
                    headers: { 'X-WP-Nonce': '<?php echo esc_js(wp_create_nonce('wp_rest')); ?>' }
                })
                    .then(function (r) { return r.json(); })
                    .then(function (d) {
                        out.style.display = 'block';
                        out.textContent = JSON.stringify(d, null, 2);
                    })
                    .catch(function (e) {
                        out.style.display = 'block';
                        out.textContent = '请求失败: ' + e.message;
                    })
                    .finally(function () { btn.disabled = false; });
            });
        })();
        </script>
    </div>
    <?php
}

add_action('admin_enqueue_scripts', 'geo_wp_admin_scripts');

function geo_wp_admin_scripts($hook_suffix)
{
    if (!in_array($hook_suffix, array('post.php', 'post-new.php'), true)) {
        return;
    }
    wp_enqueue_script('geo-wp-editor', plugins_url('geo-editor.js', __FILE__), array('wp-api-fetch', 'wp-plugins', 'wp-edit-post', 'wp-element', 'wp-components'), GEO_WP_VERSION, true);
    wp_localize_script('geo-wp-editor', 'GeoWP', array(
        'restUrl'  => rest_url('geo/v1'),
        'nonce'    => wp_create_nonce('wp_rest'),
        'postId'   => get_the_ID(),
        'threshold' => GEO_WP_DEFAULT_OK_THRESHOLD,
    ));
}

add_action('init', function () {
    if (function_exists('wp_set_script_translations')) {
        wp_set_script_translations('geo-wp-editor', 'geo-wp', dirname(__FILE__) . '/languages');
    }
});
