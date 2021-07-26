import React, { useState, useEffect } from 'react';
import { BrowserRouter as Router, Route, Link, useLocation, useHistory } from 'react-router-dom';
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Legend } from 'recharts';
import { randomColor } from 'randomcolor';

import './App.css';

function runs(x) {
  return x.pass + x.flake + x.fail;
}

function passRate(x, passMode, fallback) {
  const total = x.pass + x.flake + x.fail;
  if (total === 0) {
    return fallback;
  }
  if (passMode === 'success') {
    return x.pass/total;
  } else if (passMode === 'flake') {
    return x.flake/total;
  } else if (passMode === 'success+flake') {
    return (x.pass + x.flake)/total;
  }
  return 0;
}

function passRateChange(x, passMode) {
  const curr = passRate(x[0], passMode, 1), prev = passRate(x[1], passMode, 1);
  return curr - prev;
}

function jobState(x, passMode) {
  let rate = passRate(x, passMode, -1);
  if (rate === -1) {
    return 'nodata';
  }
  if (passMode === 'flake') {
    rate = 1 - rate;
  }
  if (rate === 0) {
    return 'permafail';
  }
  if (rate < .6) {
    return 'red';
  }
  if (rate < .8) {
    return 'yellow';
  }
  return 'green';
}

function PassRate({ jobValue, passMode }) {
  const rate = passRate(jobValue, passMode, -1);
  let value = 'n/a';
  if (rate !== -1) {
    value = (rate*100).toFixed(2) + '%';
  }
  return (
    <span className="rate"><span className="percent numeric">{value}</span> ({runs(jobValue)} runs)</span>
  );
}

function PassRateChange({ jobValues, passMode }) {
  const curr = passRate(jobValues[0], passMode, -1), prev = passRate(jobValues[1], passMode, -1);
  let className, symbol;
  if (curr === -1 || prev === -1) {
    className = 'na';
    symbol = '?';
  } else if (Math.abs(curr - prev) < .02) {
    className = 'neutral';
    symbol = '-';
  } else if (curr - prev < 0) {
    className =  passMode === 'flake' ? 'positive' : 'negative';
    symbol = '⬇';
  } else {
    className = passMode === 'flake' ? 'negative' : 'positive';
    symbol = '⬆';
  }
  return (
    <span className={className}>{symbol}</span>
  );
}

function getSortFunc(sortBy, passMode) {
  if (sortBy === 'name') {
    return (a, b) => {
      const aa = a.columns[0], bb = b.columns[0];
      return aa < bb ? -1 : +(aa > bb);
    }
  } else if (sortBy === 'currentPassRate') {
    return (a, b) => {
      return passRate(a.values[0], passMode, -1) - passRate(b.values[0], passMode, -1);
    }
  } else if (sortBy === 'previousPassRate') {
    return (a, b) => {
      return passRate(a.values[1], passMode, -1) - passRate(b.values[1], passMode, -1);
    }
  } else if (sortBy === 'passRateChange') {
    return (a, b) => {
      let c = passRateChange(a.values, passMode) - passRateChange(b.values, passMode);
      if (c !== 0) {
        return c;
      }
      return passRate(a.values[0], passMode, -1) - passRate(b.values[0], passMode, -1);
    }
  }
}

function useQueryParam(paramName, defaultValue) {
  const history = useHistory();
  const search = useLocation().search;
  const query = new URLSearchParams(search);
  const value = query.get(paramName) || defaultValue;
  return [value, newValue => {
    query.set(paramName, newValue);
    const newSearch = query.toString();
    if (search !== newSearch) {
      history.push('?' + newSearch);
    }
  }];
}

function SippyTable({ data, passMode, columns, filter, sortBy, periods, testName }) {
  return (
    <table>
      <tbody>
        {data.map((row, i) => {
          return (
            <tr key={i} className={'job-'+jobState(row.values[0], passMode)}>
              <td>{
                columns === 'sippytags' ? <Link to={'?columns=sippytags&filter=' + filter + (filter ? ' ' : '') + row.columns[0] + '&sortby=' + sortBy + '&periods=' + periods + '&testname=' + encodeURIComponent(testName) }>{row.columns[0]}</Link> :
                columns === 'name,dashboard' ? <a target="_blank" rel="noreferrer" href={'https://testgrid.k8s.io/' + row.columns[1] + '#' + row.columns[0]}>{row.columns[0]}</a> :
                row.columns[0]
              }</td>
              <td><PassRate jobValue={row.values[0]} passMode={passMode} /></td>
              <td className="trend"><PassRateChange jobValues={row.values} passMode={passMode} /></td>
              <td><PassRate jobValue={row.values[1]} passMode={passMode} /></td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

function mixColors(a, b, q) {
  let ar = a.slice(1,3), ag = a.slice(3,5), ab = a.slice(5,7);
  let br = b.slice(1,3), bg = b.slice(3,5), bb = b.slice(5,7);
  let rr = Math.round(parseInt(ar, 16)*q + parseInt(br, 16)*(1-q));
  let rg = Math.round(parseInt(ag, 16)*q + parseInt(bg, 16)*(1-q));
  let rb = Math.round(parseInt(ab, 16)*q + parseInt(bb, 16)*(1-q));
  return '#' + rr.toString(16).padStart(2, '0') + rg.toString(16).padStart(2, '0') + rb.toString(16).padStart(2, '0');
}

function Chart({ data, passMode }) {
  let [selected, setSelected] = useState('');

  if (data.length === 0) {
    return '';
  }

  let columns = [];
  for (let i = 0; i < data[0].values.length; i++) {
    let values = {x: -data[0].values.length + i + 1};
    data.forEach(row => {
      values[row.columns[0]] = Math.round(passRate(row.values[row.values.length - i - 1], passMode, NaN)*10000)/100;
    });
    columns.push(values);
  }

  let colors = randomColor({ count: data.length, luminosity: 'dark', seed: 0 });

  return (
    <LineChart
      width={800}
      height={400}
      data={columns}
      margin={{ top: 5, right: 30, left: 20, bottom: 5 }}
    >
      <CartesianGrid strokeDasharray="3 3" />
      <XAxis dataKey="x" />
      <YAxis />
      <Legend height={36}
        onMouseEnter={(o) => { setSelected(o.dataKey); }}
        onMouseLeave={(o) => { setSelected(''); }} />
      {data.map((row, i) => <Line type="monotone" stroke={selected === '' || row.columns[0] === selected ? colors[i] : mixColors(colors[i], '#ffffff', 0.2)} dataKey={row.columns[0]} strokeOpacity={selected === '' || row.columns[0] === selected ? 1 : 0.5}
        onMouseEnter={() => { setSelected(row.columns[0]); }}
        onMouseLeave={() => { setSelected(''); }} /> )}
    </LineChart>
  );
}

function heatmapColor(x, passMode) {
  if (isNaN(x)) {
    return 'nodata';
  }
  if (passMode === 'flake') {
    x = 100 - x;
  }
  if (x === 0) {
    return 'zero';
  }
  if (x < 40) {
    return '00';
  }
  if (x < 60) {
    return '40';
  }
  if (x < 80) {
    return '60';
  }
  return '80';
}

function heatmapValue(x) {
  if (isNaN(x)) {
    return '-';
  }
  return x;
}

function Heatmap({ data, passMode, columns, filter, sortBy, periods, testName }) {
  if (data.length === 0) {
    return '';
  }

  let cols = [];
  for (let i = 0; i < data[0].values.length; i++) {
    let values = {x: -data[0].values.length + i + 1};
    data.forEach(row => {
      values[row.columns[0]] = Math.round(passRate(row.values[row.values.length - i - 1], passMode, NaN)*100);
    });
    cols.push(values);
  }

  return (
    <table>
      <thead>
        <th>name</th>
        {cols.map((col, i) => <th>{i - data[0].values.length + 1}</th>)}
      </thead>
      <tbody>
        {data.map(row => <tr>
          <td>{
            columns === 'sippytags' ? <Link to={'?columns=sippytags&filter=' + filter + (filter ? ' ' : '') + row.columns[0] + '&sortby=' + sortBy + '&periods=' + periods + '&testname=' + encodeURIComponent(testName) }>{row.columns[0]}</Link> :
            columns === 'name,dashboard' ? <a target="_blank" rel="noreferrer" href={'https://testgrid.k8s.io/' + row.columns[1] + '#' + row.columns[0]}>{row.columns[0]}</a> :
            row.columns[0]
          }</td>
          {cols.map(col => <td className={'heatmap-cell heatmap-cell-' + heatmapColor(col[row.columns[0]], passMode)}>
            {heatmapValue(col[row.columns[0]])}
          </td>)}
        </tr>)}
      </tbody>
    </table>
  );
}

function Main(props) {
  const [state, setState] = useState('loading');
  const [columns, setColumns] = useQueryParam('columns', 'sippytags');
  const [filter, setFilter] = useQueryParam('filter', '');
  const [periodsParam, setPeriods] = useQueryParam('periods', '7,7');
  const [sortBy, setSortBy] = useQueryParam('sortby', 'currentPassRate');
  const [testName, setTestName] = useQueryParam('testname', '');
  const [passMode, setPassMode] = useQueryParam('passmode', 'success+flake');
  const [rawData, setRawData] = useState([]);
  const [data, setData] = useState([]);
  const location = useLocation();

  let periods = periodsParam;
  let mode = 'sippytable';
  let idx = periods.indexOf(':');
  if (idx !== -1) {
    mode = periods.slice(idx + 1);
    periods = periods.slice(0, idx);
  } else if ((periods.match(/,/g) || []).length > 1) {
    mode = 'chart';
  }

  useEffect(() => {
    setState('loading');
    // TODO: cancel previous request
    fetch('/api/builds?columns=' + columns + '&filter=' + filter + '&periods=' + periods + '&testname=' + encodeURIComponent(testName))
      .then(response => {
        if (response.status !== 200) {
          throw new Error(response.statusText);
        }
        return response.json();
      })
      .then(data => {
        setState('loaded');
        setRawData(data.data);
      })
      .catch(error => {
        setState('error:' + error.toString());
      });
  }, [columns, filter, periods, testName]);

  useEffect(() => {
    let data = [...rawData];
    data.sort(getSortFunc(sortBy, passMode));
    setData(data);
  }, [rawData, passMode, sortBy]);

  let content = [];
  if (state === 'loading') {
    content = <div>Loading...</div>;
  } else if (state.startsWith('error:')) {
    content = <div>Failed to load data: {state.slice('error:'.length)}; <a href={location.pathname + location.search}>reload</a></div>;
  } else if (state === 'loaded') {
    if (mode === 'chart') {
      content = <Chart data={data} passMode={passMode} />;
    } else if (mode === 'heatmap') {
      content = <Heatmap data={data} passMode={passMode} columns={columns} filter={filter} sortBy={sortBy} periods={periodsParam} testName={testName} />;
    } else {
      content = <SippyTable data={data} passMode={passMode} columns={columns} filter={filter} sortBy={sortBy} periods={periodsParam} testName={testName} />;
    }
  }

  return (
    <div className="app">
      <div className="filter">
        <select value={columns} onChange={ev => { setColumns(ev.target.value); }}>
          <option value="sippytags">Sippy Variants</option>
          <option value="name,dashboard">Job Names</option>
          <option value="test">Tests</option>
        </select>
        <input type="text" placeholder="filter, for example: aws -upgrade" value={filter} onChange={ev => { setFilter(ev.target.value); }} />
        <select value={sortBy} onChange={ev => { setSortBy(ev.target.value); }}>
          <option value="name">Sort by name</option>
          <option value="currentPassRate">Sort by pass rate (current period)</option>
          <option value="previousPassRate">Sort by pass rate (previous period)</option>
          <option value="passRateChange">Sort by pass rate change</option>
        </select>
        <select value={periodsParam} onChange={ev => { setPeriods(ev.target.value); }}>
          <option value="7,60">last 7 days, previous 60 days</option>
          <option value="14,14">last 14 days, previous 14 days</option>
          <option value="7,21">last 7 days, previous 21 days</option>
          <option value="3,25">last 3 days, previous 25 days</option>
          <option value="7,7">last 7 days, previous 7 days</option>
          <option value="2,12">last 2 days, previous 12 days</option>
          <option value="1,13">last 24 hours, previous 13 days</option>
          <option value="1,1">last 24 hours, previous 24 hours</option>
          <option value="1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1">chart</option>
          <option value="7,7,7,7">chart (weekly)</option>
          <option value="1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1:heatmap">heatmap</option>
        </select>
        <br />
        Test name: <input type="text" placeholder="Overall" value={testName} onChange={ev => { setTestName(ev.target.value); }} />
        <select value={passMode} onChange={ev => { setPassMode(ev.target.value); }}>
          <option value="success+flake">Successes + Flakes</option>
          <option value="success">Successes only</option>
          <option value="flake">Flakes only</option>
        </select>
      </div>
      {content}
    </div>
  );
}

function App() {
  return (
    <Router>
      <Route exact path="/" component={Main} />
    </Router>
  );
}

export default App;
