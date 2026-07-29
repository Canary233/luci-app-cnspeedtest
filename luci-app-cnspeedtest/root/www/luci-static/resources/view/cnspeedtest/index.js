'use strict';
'require view';
'require rpc';
'require ui';

var callInterfaces = rpc.declare({
	object: 'cnspeedtest',
	method: 'interfaces',
	expect: { '': { interfaces: [] } }
});

var callNodes = rpc.declare({
	object: 'cnspeedtest',
	method: 'nodes',
	params: [ 'interface' ],
	expect: { '': { nodes: [] } }
});

var callStart = rpc.declare({
	object: 'cnspeedtest',
	method: 'start',
	params: [ 'interface', 'duration', 'download_threads', 'upload_threads', 'no_download', 'no_upload', 'host', 'port', 'name' ],
	expect: { '': {} }
});

var callStatus = rpc.declare({
	object: 'cnspeedtest',
	method: 'status',
	params: [ 'id' ],
	expect: { '': {} }
});

var callStop = rpc.declare({
	object: 'cnspeedtest',
	method: 'stop',
	params: [ 'id' ],
	expect: { '': {} }
});

function formatMbps(value) {
	value = Number(value || 0);
	return value.toFixed(2) + ' Mbps';
}

function formatMs(value) {
	return Number(value || 0).toFixed(0) + ' ms';
}

function clearNode(node) {
	while (node.firstChild)
		node.removeChild(node.firstChild);
}

function setResult(target, node) {
	clearNode(target);
	target.appendChild(node);
}

function statusPanel(text) {
	return E('div', {
		'class': 'cbi-section',
		style: 'padding:1.2em;text-align:center;color:#667085'
	}, text);
}

function metricTile(title, value, note, borderColor) {
	var valueNode = E('div', { style: 'font-size:24px;font-weight:700;color:#1f2937;line-height:1.2' }, value);
	var tile = E('div', {
		style: [
			'border-left:4px solid ' + borderColor,
			'background:#fff',
			'border-radius:6px',
			'box-shadow:0 1px 3px rgba(0,0,0,.08)',
			'padding:14px 16px',
			'min-width:150px',
			'flex:1 1 150px'
		].join(';')
	}, [
		E('div', { style: 'font-size:13px;color:#667085;margin-bottom:8px' }, title),
		valueNode,
		E('div', { style: 'font-size:12px;color:#98a2b3;margin-top:6px' }, note || '')
	]);
	tile.valueNode = valueNode;
	return tile;
}

function infoRow(label, value) {
	return E('tr', {}, [
		E('td', { style: 'width:8em;color:#667085;padding:7px 10px;border-top:1px solid #edf0f3' }, label),
		E('td', { style: 'padding:7px 10px;border-top:1px solid #edf0f3;word-break:break-all' }, value || '-')
	]);
}

function parseNodeValue(value) {
	if (!value)
		return null;
	var parts = String(value).split('|');
	return {
		host: parts[0] || '',
		port: parseInt(parts[1] || '0', 10) || 65499,
		name: decodeURIComponent(parts.slice(2).join('|') || '')
	};
}

function encodeNodeValue(item) {
	return [ item.host, item.port || 65499, encodeURIComponent(item.name || '') ].join('|');
}

function fillNodeSelect(select, nodes) {
	clearNode(select);
	select.appendChild(E('option', { value: '' }, '自动选择'));
	(nodes || []).forEach(function(item) {
		var label = item.name || item.host;
		if (item.latency_ms)
			label += ' (' + item.latency_ms + ' ms)';
		select.appendChild(E('option', {
			value: encodeNodeValue(item)
		}, label));
	});
}

function renderError(text) {
	return E('div', {
		'class': 'alert-message warning',
		style: 'margin-top:1em'
	}, text || '测速失败。');
}

function sortedValues(samples) {
	var values = [];
	for (var i = 0; i < samples.length; i++) {
		var v = Number(samples[i].mbps || 0);
		if (v > 0 && isFinite(v))
			values.push(v);
	}
	values.sort(function(a, b) { return a - b; });
	return values;
}

function percentile(values, ratio) {
	if (!values.length)
		return 0;
	var index = Math.floor((values.length - 1) * ratio);
	return values[index] || 0;
}

function averageValue(samples) {
	var sum = 0;
	var count = 0;
	for (var i = 0; i < samples.length; i++) {
		var v = Number(samples[i].mbps || 0);
		if (v >= 0 && isFinite(v)) {
			sum += v;
			count++;
		}
	}
	return count ? sum / count : 0;
}

function smoothSamples(samples) {
	var result = [];
	var radius = 2;
	for (var i = 0; i < samples.length; i++) {
		var start = Math.max(0, i - radius);
		var end = Math.min(samples.length - 1, i + radius);
		var sum = 0;
		var count = 0;
		for (var j = start; j <= end; j++) {
			var value = Number(samples[j].mbps || 0);
			if (value >= 0 && isFinite(value)) {
				sum += value;
				count++;
			}
		}
		result.push({
			elapsed_ms: Number(samples[i].elapsed_ms || 0),
			mbps: count ? sum / count : 0
		});
	}
	return result;
}

function displayPeak(samples, finalAverage) {
	var values = sortedValues(samples);
	if (!values.length)
		return 0;
	var p90 = percentile(values, 0.90);
	var avg = Number(finalAverage || 0) || averageValue(samples);
	if (avg <= 0)
		return p90;
	return Math.max(avg, Math.min(p90, avg * 1.12));
}

function chartScale(samples, finalAverage) {
	var peak = displayPeak(samples, finalAverage);
	if (peak <= 0)
		peak = 1;
	return peak * 1.18;
}

function makeChart(title, color) {
	var canvas = E('canvas', {
		width: '720',
		height: '190',
		style: 'width:100%;height:190px;display:block;background:#fafbfc;border:1px solid #eef2f6;border-radius:6px'
	});
	var peakNode = E('span', { style: 'color:#667085' }, '稳定峰值 0.00 Mbps');
	var phaseNode = E('span', { style: 'color:#98a2b3;font-size:12px;margin-left:10px' }, '等待数据');
	var root = E('div', {
		'class': 'cbi-section',
		style: 'margin-top:14px;background:#fff;padding:14px 16px'
	}, [
		E('div', { style: 'display:flex;justify-content:space-between;align-items:baseline;margin-bottom:8px;gap:12px;flex-wrap:wrap' }, [
			E('div', {}, [ E('strong', {}, title), phaseNode ]),
			peakNode
		]),
		canvas
	]);

	function draw(samples, finalAverage, active) {
		var ctx = canvas.getContext && canvas.getContext('2d');
		if (!ctx)
			return;
		var displaySamples = smoothSamples(samples || []);
		var cssWidth = canvas.clientWidth || 720;
		var cssHeight = 190;
		var dpr = window.devicePixelRatio || 1;
		var targetWidth = Math.max(320, Math.floor(cssWidth * dpr));
		var targetHeight = Math.floor(cssHeight * dpr);
		if (canvas.width !== targetWidth || canvas.height !== targetHeight) {
			canvas.width = targetWidth;
			canvas.height = targetHeight;
		}
		ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
		ctx.clearRect(0, 0, cssWidth, cssHeight);
		ctx.fillStyle = '#fafbfc';
		ctx.fillRect(0, 0, cssWidth, cssHeight);

		var padLeft = 34;
		var padRight = 12;
		var padTop = 16;
		var padBottom = 24;
		var w = cssWidth - padLeft - padRight;
		var h = cssHeight - padTop - padBottom;
		ctx.strokeStyle = '#edf0f3';
		ctx.lineWidth = 1;
		ctx.fillStyle = '#98a2b3';
		ctx.font = '11px sans-serif';
		for (var i = 0; i <= 4; i++) {
			var y = padTop + h * i / 4;
			ctx.beginPath();
			ctx.moveTo(padLeft, y);
			ctx.lineTo(cssWidth - padRight, y);
			ctx.stroke();
		}

		var yMax = chartScale(displaySamples, finalAverage);
		var peak = displayPeak(displaySamples, finalAverage);
		peakNode.textContent = '稳定峰值 ' + formatMbps(peak);
		phaseNode.textContent = displaySamples.length ? (active ? '实时绘制中' : '测速完成') : '等待数据';

		ctx.fillText(formatMbps(yMax), 4, padTop + 4);
		ctx.fillText('0', 18, padTop + h + 4);

		if (!displaySamples.length) {
			ctx.fillStyle = '#667085';
			ctx.fillText('开始测速后实时绘制波形', padLeft + 8, padTop + 28);
			return;
		}

		var maxT = Number(displaySamples[displaySamples.length - 1].elapsed_ms || 1);
		if (maxT <= 0)
			maxT = 1;
		ctx.strokeStyle = color;
		ctx.lineWidth = 2.5;
		ctx.lineJoin = 'round';
		ctx.lineCap = 'round';
		ctx.beginPath();
		for (var j = 0; j < displaySamples.length; j++) {
			var sample = displaySamples[j];
			var mbps = Math.min(Number(sample.mbps || 0), yMax);
			var x = padLeft + (Number(sample.elapsed_ms || 0) / maxT) * w;
			var yy = padTop + (1 - mbps / yMax) * h;
			if (j === 0)
				ctx.moveTo(x, yy);
			else
				ctx.lineTo(x, yy);
		}
		ctx.stroke();

		var last = displaySamples[displaySamples.length - 1];
		var lastX = padLeft + (Number(last.elapsed_ms || 0) / maxT) * w;
		var lastY = padTop + (1 - Math.min(Number(last.mbps || 0), yMax) / yMax) * h;
		ctx.fillStyle = color;
		ctx.beginPath();
		ctx.arc(lastX, lastY, 3, 0, Math.PI * 2);
		ctx.fill();
	}

	return {
		root: root,
		draw: draw
	};
}

function parseEvents(log) {
	var events = [];
	var lines = String(log || '').split(/\n/);
	for (var i = 0; i < lines.length; i++) {
		var line = lines[i].replace(/^\s+|\s+$/g, '');
		if (!line)
			continue;
		try {
			events.push(JSON.parse(line));
		} catch (e) {}
	}
	return events;
}

function latestSpeed(samples) {
	if (!samples.length)
		return 0;
	var smoothed = smoothSamples(samples);
	return Number(smoothed[smoothed.length - 1].mbps || 0);
}

function createLiveView(testDownload, testUpload) {
	var downTile = metricTile('下载速度', '0.00 Mbps', 'Download', '#0ea5e9');
	var upTile = metricTile('上传速度', '0.00 Mbps', 'Upload', '#10b981');
	var pingTile = metricTile('延迟', '0 ms', 'Ping', '#f59e0b');
	var jitterTile = metricTile('抖动', '0 ms', 'Jitter', '#8b5cf6');
	var downChart = makeChart('下载波形', '#0ea5e9');
	var upChart = makeChart('上传波形', '#10b981');
	var detail = E('div', {});
	var status = E('div', { style: 'margin-top:12px;color:#667085' }, '正在启动测速...');
	var children = [
		E('div', { style: 'display:flex;flex-wrap:wrap;gap:12px;margin-bottom:14px' }, [
			downTile,
			upTile,
			pingTile,
			jitterTile
		])
	];
	if (testDownload)
		children.push(downChart.root);
	if (testUpload)
		children.push(upChart.root);
	children.push(status);
	children.push(detail);
	var root = E('div', { style: 'margin-top:1em' }, children);

	var state = {
		download: [],
		upload: [],
		result: null,
		error: null
	};

	function renderDetail(result) {
		var node = (result && result.node) || {};
		setResult(detail, E('table', {
			'class': 'table',
			style: 'width:100%;margin-top:14px;background:#fff;border-collapse:collapse'
		}, [
			infoRow('测速节点', result.server_name || node.name),
			infoRow('服务器', node.host ? node.host + ':' + node.port : ''),
			infoRow('公网 IP', result.public_ip),
			infoRow('位置', result.location),
			infoRow('运营商', result.carrier)
		]));
	}

	function update(events, running) {
		state.download = [];
		state.upload = [];
		state.result = null;
		state.error = null;
		for (var i = 0; i < events.length; i++) {
			var event = events[i] || {};
			if (event.type === 'sample') {
				var sample = { elapsed_ms: Number(event.elapsed_ms || 0), mbps: Number(event.mbps || 0) };
				if (event.phase === 'download')
					state.download.push(sample);
				else if (event.phase === 'upload')
					state.upload.push(sample);
			} else if (event.type === 'result') {
				state.result = event.result || {};
			} else if (event.type === 'error') {
				state.error = event.error || '测速失败。';
			}
		}

		var result = state.result || {};
		downTile.valueNode.textContent = formatMbps(result.download_mbps || latestSpeed(state.download));
		upTile.valueNode.textContent = formatMbps(result.upload_mbps || latestSpeed(state.upload));
		pingTile.valueNode.textContent = formatMs(result.ping_ms);
		jitterTile.valueNode.textContent = formatMs(result.jitter_ms);
		if (testDownload)
			downChart.draw(state.download, result.download_mbps, running);
		if (testUpload)
			upChart.draw(state.upload, result.upload_mbps, running);

		if (state.error) {
			status.textContent = state.error;
			status.style.color = '#b42318';
		} else if (running) {
			status.textContent = '测速中，波形正在实时绘制...';
			status.style.color = '#667085';
		} else if (state.result) {
			status.textContent = '测速完成。';
			status.style.color = '#667085';
			renderDetail(state.result);
		} else {
			status.textContent = '测速结束，但未返回结果。';
			status.style.color = '#b42318';
		}
	}

	function initialDraw() {
		if (testDownload)
			downChart.draw([], 0, true);
		if (testUpload)
			upChart.draw([], 0, true);
	}

	return {
		root: root,
		update: update,
		initialDraw: initialDraw,
		state: state
	};
}

return view.extend({
	load: function() {
		return Promise.all([
			callInterfaces(),
			callNodes('')
		]);
	},

	render: function(data) {
		var ifaceData = data[0] || {};
		var nodeData = data[1] || {};
		var list = ifaceData.interfaces || [];
		var nodes = nodeData.nodes || [];
		var pollingTimer = null;
		var activeJob = '';

		var iface = E('select', { 'class': 'cbi-input-select' }, [
			E('option', { value: '' }, '自动选择')
		]);
		list.forEach(function(item) {
			iface.appendChild(E('option', { value: item.name }, item.title || item.name));
		});

		var nodeSelect = E('select', { 'class': 'cbi-input-select' }, []);
		fillNodeSelect(nodeSelect, nodes);

		var duration = E('input', { 'class': 'cbi-input-text', type: 'number', min: '1', max: '30', value: '8' });
		var downThreads = E('input', { 'class': 'cbi-input-text', type: 'number', min: '1', max: '32', value: '8' });
		var upThreads = E('input', { 'class': 'cbi-input-text', type: 'number', min: '1', max: '32', value: '4' });
		var testDownload = E('input', { type: 'checkbox', checked: true });
		var testUpload = E('input', { type: 'checkbox', checked: true });
		var output = E('div', {}, [ statusPanel('就绪。') ]);

		function stopPolling() {
			if (pollingTimer) {
				window.clearInterval(pollingTimer);
				pollingTimer = null;
			}
		}

		function refreshNodes() {
			setResult(output, statusPanel('正在加载节点...'));
			return callNodes(iface.value).then(function(resp) {
				if (resp && resp.code === 0) {
					fillNodeSelect(nodeSelect, resp.nodes || []);
					setResult(output, statusPanel('就绪。'));
				} else {
					setResult(output, renderError(resp && resp.error ? resp.error : '节点加载失败。'));
				}
			}).catch(function(err) {
				setResult(output, renderError(err && err.message ? err.message : String(err)));
			});
		}

		iface.addEventListener('change', refreshNodes);

		var refreshButton = E('button', {
			'class': 'btn cbi-button',
			click: ui.createHandlerFn(this, refreshNodes)
		}, '刷新节点');

		var stopButton = E('button', {
			'class': 'btn cbi-button',
			disabled: 'disabled',
			click: ui.createHandlerFn(this, function() {
				stopPolling();
				stopButton.disabled = true;
				button.disabled = false;
				if (activeJob)
					return callStop(activeJob);
			})
		}, '停止');

		var button = E('button', {
			'class': 'btn cbi-button cbi-button-action',
			click: ui.createHandlerFn(this, function() {
				if (!testDownload.checked && !testUpload.checked) {
					setResult(output, renderError('请至少选择一个测试项目。'));
					return;
				}
				var nodeInfo = parseNodeValue(nodeSelect.value) || {};
				var live = createLiveView(testDownload.checked, testUpload.checked);
				var noDownload = !testDownload.checked;
				var noUpload = !testUpload.checked;
				stopPolling();
				activeJob = '';
				button.disabled = true;
				stopButton.disabled = false;
				setResult(output, live.root);
				window.setTimeout(live.initialDraw, 0);

				function poll(jobId) {
					return callStatus(jobId).then(function(resp) {
						if (!resp || resp.code !== 0)
							throw new Error(resp && resp.error ? resp.error : '读取测速状态失败。');
						var events = parseEvents(resp.log || '');
						live.update(events, !!resp.running);
						if (!resp.running) {
							stopPolling();
							button.disabled = false;
							stopButton.disabled = true;
						}
					}).catch(function(err) {
						stopPolling();
						button.disabled = false;
						stopButton.disabled = true;
						setResult(output, renderError(err && err.message ? err.message : String(err)));
					});
				}

				return callStart(
					iface.value,
					parseInt(duration.value || '8', 10),
					parseInt(downThreads.value || '8', 10),
					parseInt(upThreads.value || '4', 10),
					noDownload,
					noUpload,
					nodeInfo.host || '',
					nodeInfo.port || 65499,
					nodeInfo.name || ''
				).then(function(resp) {
					if (!resp || resp.code !== 0)
						throw new Error(resp && resp.error ? resp.error : '启动测速失败。');
					activeJob = resp.id;
					pollingTimer = window.setInterval(function() {
						poll(resp.id);
					}, 500);
					poll(resp.id);
				}).catch(function(err) {
					stopPolling();
					button.disabled = false;
					stopButton.disabled = true;
					setResult(output, renderError(err && err.message ? err.message : String(err)));
				});
			})
		}, '开始测速');

		return E('div', { 'class': 'cbi-map' }, [
			E('h2', {}, '宽带测速'),
			E('div', { 'class': 'cbi-section' }, [
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, '出口接口'),
					E('div', { 'class': 'cbi-value-field' }, iface)
				]),
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, '测试节点'),
					E('div', { 'class': 'cbi-value-field' }, [ nodeSelect, ' ', refreshButton ])
				]),
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, '测速时长'),
					E('div', { 'class': 'cbi-value-field' }, [ duration, ' 秒' ])
				]),
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, '下载线程数'),
					E('div', { 'class': 'cbi-value-field' }, downThreads)
				]),
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, '上传线程数'),
					E('div', { 'class': 'cbi-value-field' }, upThreads)
				]),
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, '测试项目'),
					E('div', { 'class': 'cbi-value-field' }, [
						E('label', {}, [ testDownload, ' 下载测速' ]),
						' ',
						E('label', {}, [ testUpload, ' 上传测速' ])
					])
				]),
				E('div', { 'class': 'cbi-value' }, [
					E('label', { 'class': 'cbi-value-title' }, ''),
					E('div', { 'class': 'cbi-value-field' }, [ button, ' ', stopButton ])
				])
			]),
			output
		]);
	}
});
