{
  /**
   * Return an InfluxDB Target
   *
   * @name influxdb.target
   *
   * @param query Raw InfluxQL statement
   *
   * @param alias 'Alias By' pattern
   * @param datasource Datasource
   *
   * @param rawQuery Enable/disable raw query mode
   *
   * @param policy Tagged query 'From' policy
   * @param measurement Tagged query 'From' measurement
   * @param group_time 'Group by' time condition (if set to null, do not groups by time)
   * @param group_tags 'Group by' tags list
   * @param fill 'Group by' missing values fill mode (works only with 'Group by time()')
   *
   * @param resultFormat Format results as 'Time series' or 'Table'
   *
   * @return Panel target
   */
  target(
    query=null,

    alias=null,
    datasource=null,

    rawQuery=null,

    policy='default',
    measurement=null,

    group_time='$__interval',
    group_tags=[],
    fill='none',

    resultFormat='time_series',
  ):: {
    local it = self,

    [if alias != null then 'alias']: alias,
    [if datasource != null then 'datasource']: datasource,

    [if query != null then 'query']: query,
    [if rawQuery != null then 'rawQuery']: rawQuery,
    [if rawQuery == null && query != null then 'rawQuery']: true,

    policy: policy,
    [if measurement != null then 'measurement']: measurement,
    tags: [],
    select: [],
    groupBy:
      if group_time != null then
        [{ type: 'time', params: [group_time] }] +
        [{ type: 'tag', params: [tag_name] } for tag_name in group_tags] +
        [{ type: 'fill', params: [fill] }]
      else
        [{ type: 'tag', params: [tag_name] } for tag_name in group_tags],

    resultFormat: resultFormat,

    where(key, operator, value, condition=null):: self {
      /*
       * Adds query tag condition ('Where' section)
       */
      tags:
        if std.length(it.tags) == 0 then
          [{ key: key, operator: operator, value: value }]
        else
          it.tags + [{
            key: key,
            operator: operator,
            value: value,
            condition: if condition == null then 'AND' else condition,
          }],
    },

    selectField(value):: self {
      /*
       * Adds InfluxDB selection ('field(value)' part of 'Select' statement)
       */
      select+: [[{ params: [value], type: 'field' }]],
    },

    addConverter(type, params=[]):: self {
      /*
       * Appends converter (aggregation, selector, etc.) to last added selection
       */
      local len = std.length(it.select),
      select:
        if len == 1 then
          [it.select[0] + [{ params: params, type: type }]]
        else if len > 1 then
          it.select[0:(len - 1)] + [it.select[len - 1] + [{ params: params, type: type }]]
        else
          [],
    },
  },
}
