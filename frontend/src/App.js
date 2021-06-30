import React, { Component } from 'react';
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
    return "nodata";
  }
  if (rate === 0) {
    return "permafail";
  }
  if (rate < .6) {
    return "red";
  }
  if (rate < .8) {
    return "yellow";
  }
  return "green";
}

function showDelta(d) {
  if (d < 0) {
    return "⬇";
  }
  if (d > 0) {
    return "⬆";
  }
  return "-";
}

function classDelta(d) {
  if (d < 0) {
    return "negative";
  }
  if (d > 0) {
    return "positive";
  }
  return "neutral";
}

function passDelta(prev, curr) {
  const p = passRate(prev, -1), c = passRate(curr, -1);
  if (p === -1 || c === -1) {
    return 0;
  }
  if (Math.abs(c - p) < .02) {
    return 0;
  }
  return c - p;
}

function formatRate(x) {
  if (x === -1) {
    return "n/a";
  }
  return (x*100).toFixed(2) + "%";
}

class App extends Component {
  constructor(props) {
    super(props);

    this.state = {
      data: [],
      columns: "sippytags",
      filter: "",
      sortBy: "currentPassRate",
      periods: "7,7",
    };
  }

  componentDidMount() {
    this.handleFilterChange(this.state.filter);
  }

  refreshData() {
    const columns = this.state.columns, filter = this.state.filter, periods = this.state.periods;
    fetch('/api/builds?columns=' + columns + '&filter=' + filter + '&periods=' + periods)
      .then(response => response.json())
      .then(data => {
        if (this.state.columns === columns && this.state.filter === filter && this.state.periods === periods) {
          this.updateData(data.data);
        }
      });
  }

  sortFunc() {
    if (this.state.sortBy === "currentPassRate") {
      return (a, b) => {
        return passRate(a.values[0], -1) - passRate(b.values[0], -1);
      }
    } else if (this.state.sortBy === "previousPassRate") {
      return (a, b) => {
        return passRate(a.values[1], -1) - passRate(b.values[1], -1);
      }
    } else if (this.state.sortBy === "passRateChange") {
      return (a, b) => {
        let c = passRateChange(a.values) - passRateChange(b.values);
        if (c !== 0) {
          return c;
        }
        return passRate(a.values[0], -1) - passRate(b.values[0], -1);
      }
    }
  }

  updateData(data) {
    data.sort(this.sortFunc());
    this.setState({data: data});
  }

  handleColumnsChange(value) {
    this.setState({columns: value}, () => {
      this.refreshData();
    });
  }

  handleFilterChange(value) {
    this.setState({filter: value}, () => {
      this.refreshData();
    });
  }

  handleSortByChange(value) {
    this.setState({sortBy: value}, () => {
      this.updateData(this.state.data);
    });
  }

  handlePeriodsChange(value) {
    this.setState({periods: value}, () => {
      this.refreshData();
    });
  }

  render() {
    return (
      <div className="app">
        <div className="filter">
          <select onChange={ev => { this.handleColumnsChange(ev.target.value); }}>
            <option value="sippytags">Sippy Variants</option>
            <option value="name">Job Names</option>
            <option value="test">Tests</option>
          </select>
          <input type="text" placeholder="filter, for example: aws -upgrade" value={this.state.filter} onChange={ev => { this.handleFilterChange(ev.target.value); }} />
          <select onChange={ev => { this.handleSortByChange(ev.target.value); }}>
            <option value="currentPassRate">Sort by pass rate (this week)</option>
            <option value="previousPassRate">Sort by pass rate (previous week)</option>
            <option value="passRateChange">Sort by pass rate change</option>
          </select>
          <select onChange={ev => { this.handlePeriodsChange(ev.target.value); }}>
            <option value="7,7">last 7 days, previous 7 days</option>
            <option value="2,12">last 2 days, previous 12 days</option>
            <option value="1,13">last 24 hours, previous 13 days</option>
            <option value="1,1">last 24 hours, previous 24 hours</option>
          </select>
        </div>
        <table>
          <tbody>
            {this.state.data.map((row, i) => {
              return (
                <tr key={i} className={"job-"+jobState(row.values[0])}>
                  <td>{row.columns[0]}</td>
                  <td className="group-start numeric">{formatRate(passRate(row.values[0], -1))}</td>
                  <td>({runs(row.values[0])} runs)</td>
                  <td className={"trend trend-"+classDelta(passDelta(row.values[1], row.values[0]))}>{showDelta(passDelta(row.values[1], row.values[0]))}</td>
                  <td className="group-start numeric">{formatRate(passRate(row.values[1], -1))}</td>
                  <td>({runs(row.values[1])} runs)</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    );
  }
}

export default App;
