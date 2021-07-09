import React, { useState, useEffect } from 'react';
import { BrowserRouter as Router, Route, Link, useLocation, useHistory } from 'react-router-dom';
import './App.css';

function runs(x) {
  return x.pass + x.fail;
}

function passRate(x, fallback) {
  const total = x.pass + x.fail;
  if (total === 0) {
    return fallback;
  }
  return x.pass/total;
}

function passRateChange(x) {
  const curr = passRate(x[0], 1), prev = passRate(x[1], 1);
  return curr - prev;
}

function jobState(x) {
  const rate = passRate(x, -1);
  if (rate === -1) {
    return 'nodata';
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

function PassRate({ jobValue }) {
  const rate = passRate(jobValue, -1);
  let value = 'n/a';
  if (rate !== -1) {
    value = (rate*100).toFixed(2) + '%';
  }
  return (
    <span className="rate"><span className="percent numeric">{value}</span> ({runs(jobValue)} runs)</span>
  );
}

function PassRateChange({ jobValues }) {
  const curr = passRate(jobValues[0], -1), prev = passRate(jobValues[1], -1);
  let className, symbol;
  if (curr === -1 || prev === -1) {
    className = 'na';
    symbol = '?';
  } else if (Math.abs(curr - prev) < .02) {
    className = 'neutral';
    symbol = '-';
  } else if (curr - prev < 0) {
    className = 'negative';
    symbol = '⬇';
  } else {
    className = 'positive';
    symbol = '⬆';
  }
  return (
    <span className={className}>{symbol}</span>
  );
}

function getSortFunc(sortBy) {
  if (sortBy === 'currentPassRate') {
    return (a, b) => {
      return passRate(a.values[0], -1) - passRate(b.values[0], -1);
    }
  } else if (sortBy === 'previousPassRate') {
    return (a, b) => {
      return passRate(a.values[1], -1) - passRate(b.values[1], -1);
    }
  } else if (sortBy === 'passRateChange') {
    return (a, b) => {
      let c = passRateChange(a.values) - passRateChange(b.values);
      if (c !== 0) {
        return c;
      }
      return passRate(a.values[0], -1) - passRate(b.values[0], -1);
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

function Main(props) {
  const [columns, setColumns] = useQueryParam('columns', 'sippytags');
  const [filter, setFilter] = useQueryParam('filter', '');
  const [periods, setPeriods] = useQueryParam('periods', '7,7');
  const [sortBy, setSortBy] = useQueryParam('sortby', 'currentPassRate');
  const [rawData, setRawData] = useState([]);
  const [data, setData] = useState([]);

  useEffect(() => {
    // TODO: cancel previous request
    fetch('/api/builds?columns=' + columns + '&filter=' + filter + '&periods=' + periods)
      .then(response => response.json())
      .then(data => {
        setRawData(data.data);
      });
  }, [columns, filter, periods]);

  useEffect(() => {
    let data = [...rawData];
    data.sort(getSortFunc(sortBy));
    setData(data);
  }, [rawData, sortBy]);

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
          <option value="currentPassRate">Sort by pass rate (current period)</option>
          <option value="previousPassRate">Sort by pass rate (previous period)</option>
          <option value="passRateChange">Sort by pass rate change</option>
        </select>
        <select value={periods} onChange={ev => { setPeriods(ev.target.value); }}>
          <option value="7,60">last 7 days, previous 60 days</option>
          <option value="14,14">last 14 days, previous 14 days</option>
          <option value="7,21">last 7 days, previous 21 days</option>
          <option value="3,25">last 3 days, previous 25 days</option>
          <option value="7,7">last 7 days, previous 7 days</option>
          <option value="2,12">last 2 days, previous 12 days</option>
          <option value="1,13">last 24 hours, previous 13 days</option>
          <option value="1,1">last 24 hours, previous 24 hours</option>
        </select>
      </div>
      <table>
        <tbody>
          {data.map((row, i) => {
            return (
              <tr key={i} className={"job-"+jobState(row.values[0])}>
                <td>{
                  columns === "sippytags" ? <Link to={"?columns=sippytags&filter=" + filter + (filter ? " " : "") + row.columns[0] + "&sortby=" + sortBy + "&periods=" + periods}>{row.columns[0]}</Link> :
                  columns === "name,dashboard" ? <a target="_blank" rel="noreferrer" href={"https://testgrid.k8s.io/" + row.columns[1] + "#" + row.columns[0]}>{row.columns[0]}</a> :
                  row.columns[0]
                }</td>
                <td><PassRate jobValue={row.values[0]} /></td>
                <td className="trend"><PassRateChange jobValues={row.values} /></td>
                <td><PassRate jobValue={row.values[1]} /></td>
              </tr>
            );
          })}
        </tbody>
      </table>
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
